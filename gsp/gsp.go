package gsp

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/go-ble/ble"
	"github.com/go-ble/ble/linux"
)

const (
	gspServiceUUID = "34802252-7185-4d5d-b431-630e7050e8f0"
	gspWriteUUID   = "34800001-7185-4d5d-b431-630e7050e8f0"
	gspNotifyUUID  = "34800002-7185-4d5d-b431-630e7050e8f0"

	BleMTU = 247

	StatusOK = 200

	SendCtxTimeout = 1
)

// BLETransport BLE transport
type BLETransport interface {
	Write([]byte) error
	Notify() <-chan []byte
	Close() error
}

type bleTransport struct {
	log *slog.Logger

	client ble.Client
	write  *ble.Characteristic
	notify *ble.Characteristic

	notifyC chan []byte
}

func (t *bleTransport) Write(b []byte) error {
	t.log.Debug(fmt.Sprintf("%+v", b), "note", "bleTransport write bytes")
	return t.client.WriteCharacteristic(t.write, b, false)
}
func (t *bleTransport) Notify() <-chan []byte {
	return t.notifyC
}
func (t *bleTransport) Close() error {
	return t.client.CancelConnection()
}

// Parts how many data parts have a subs data packet. Depends on path and frequency.
type Parts int

const (
	PartsUnknow = iota
	PartsOne
	PartsTwo
)

// Subscription to receive data subscribed to Path via C
type Subscription struct {
	// Path path subscribed
	Path string
	// C chan to receive data
	C chan []byte

	// buf per ref buffer, needed to autolearn DATA, DATA_PART2
	buf []byte

	// dataParts how may dataParts
	// Autolearning based on initial packet sequences. FIXME: fragile
	dataParts Parts
}

func NewSubscription(path string) *Subscription {
	return &Subscription{Path: path, C: make(chan []byte, 1), dataParts: PartsUnknow, buf: make([]byte, 0)}
}

// Subscriptions map of active subscriptions
type Subscriptions map[uint8]*Subscription

type GSP struct {
	logLevel slog.Level
	ctx      context.Context

	dev     *linux.Device
	devices map[string]*Device
}

func New(ctx context.Context, slevel slog.Level, dev string) *GSP {
	g := GSP{}

	var err error

	// BLE
	g.dev, err = linux.NewDeviceWithName(dev)
	if err != nil {
		log.Panic(err)
	}
	ble.SetDefaultDevice(g.dev)

	// init
	g.ctx = ctx
	g.logLevel = slevel
	g.devices = make(map[string]*Device, 0)

	return &g
}

// AddDevice adds and init/connect to new device
// NOTE: you might wanna avoid running in parallel, BLE controller might complain.
func (g *GSP) AddDevice(addr string) (err error) {
	if _, ok := g.devices[addr]; ok {
		return fmt.Errorf("refusing to add device with add %s: already existing", addr)
	}
	g.devices[addr] = NewDevice(addr, g.logLevel)

	// context
	err = g.devices[addr].Connect(g.ctx)
	if err != nil {
		return
	}
	return
}
func (g *GSP) Close(addr string) {
	var err error
	for i := range g.devices {
		err = g.devices[i].Close()
		if err != nil {
			log.Print(err)
		}
	}
}
func (g *GSP) GetDevice(addr string) (dev *Device, err error) {
	dev, ok := g.devices[addr]
	if !ok {
		err = fmt.Errorf("no device with addr %s", addr)
		return
	}
	return
}

// Device main Device struct to interact with BLE client device
type Device struct {
	addr string

	log *slog.Logger

	transport  BLETransport
	refCounter uint8

	resp   map[uint8]chan Packet
	muResp sync.Mutex

	subs   Subscriptions
	muSubs sync.Mutex
}

func NewDevice(addr string, logLevel slog.Level) *Device {
	return &Device{
		addr: addr,
		log: slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level:     logLevel,
			AddSource: true,
		})).With("MAC", addr),
	}
}

// Connect to BLE client device having d.addr
func (d *Device) Connect(ctx context.Context) (err error) {
	// reset
	d.resp = make(map[uint8]chan Packet)
	d.subs = make(Subscriptions)

	addr := ble.NewAddr(d.addr)

	go func() {
		backoff := time.Second
		for {
			select {
			case <-ctx.Done():
				log.Print(ctx.Err())
				return
			default:
			}

			// dial
			log.Print("connecting addr: ", d.addr, "...")
			client, err := ble.Dial(ctx, addr)
			if err != nil {
				time.Sleep(backoff)
				if backoff < 10*time.Second {
					backoff *= 2
				}
				continue
			}
			backoff = time.Second

			// tune MTU
			_, err = client.ExchangeMTU(BleMTU)
			if err != nil {
			}

			// discover and get desired characteristics
			profile, err := client.DiscoverProfile(true)
			if err != nil {
				log.Print(err)
				continue
			}

			svcUUID := ble.MustParse(gspServiceUUID)
			wUUID := ble.MustParse(gspWriteUUID)
			nUUID := ble.MustParse(gspNotifyUUID)

			var writeChar *ble.Characteristic
			var notifyChar *ble.Characteristic

			for _, s := range profile.Services {
				if !s.UUID.Equal(svcUUID) {
					continue
				}
				for _, c := range s.Characteristics {
					if c.UUID.Equal(wUUID) {
						writeChar = c
					}
					if c.UUID.Equal(nUUID) {
						notifyChar = c
					}
				}
			}

			if writeChar == nil || notifyChar == nil {
				log.Print(err)
				continue
			}

			// setup notify
			notifyC := make(chan []byte, BleMTU)

			err = client.Subscribe(notifyChar, false, func(b []byte) {
				d.log.Debug(fmt.Sprintf("%+v", b), "note", "receive bytes")
				cp := make([]byte, len(b))
				copy(cp, b)
				notifyC <- cp
			})
			if err != nil {
				log.Print(err)
				continue
			}

			// setup transport
			d.transport = &bleTransport{
				log:     d.log,
				client:  client,
				write:   writeChar,
				notify:  notifyChar,
				notifyC: notifyC,
			}

			// receive loop: setup a go func to receive notified data, convert them to Packet.
			// CodeCommandResponse are pushed to resp chans, while (CodeDataStream,CodeDataStream2) from subs is pushed to subs chans
			d.handleConnection(client)
		}
	}()

	return
}

func (d *Device) handleConnection(client ble.Client) {
	for {
		select {
		case <-client.Disconnected():
			log.Print("client with addr: ", d.addr, " disconnected. Reconnecting...")
			break
		default:
			for raw := range d.transport.Notify() {
				d.log.Debug(fmt.Sprintf("%+v", raw), "note", "device receive notify bytes")

				p, err := NewPacketFromBytes(raw)
				if err != nil {
					log.Printf("cannot decode packet from %v: %v", raw, err)
					continue
				}
				d.log.Info(fmt.Sprintf("PACKET %d: %+v", len(p.Data), p))

				ref := p.Ref
				// push reponses
				switch p.Code {
				case CodeCommandResponse:
					// create chan if needed
					d.muResp.Lock()
					_, ok := d.resp[ref]
					if !ok {
						d.log.Debug(fmt.Sprintf("creating resp chan for ref %v", ref), "note", "device notify")
						d.resp[ref] = make(chan Packet, 16)
					}
					// push to chan
					select {
					case d.resp[ref] <- *p:
						d.log.Debug(fmt.Sprintf("pushed Packet %v to channel resp[%v], which has size %d", *p, ref, len(d.resp)), "note", "device notify")
					default:
						log.Printf("cannot send. Chan resp[%d] is possibly full", ref)
					}
					d.muResp.Unlock()

				case CodeDataStream:
					// NOTE: assuming DataStream2 are sent adjacent to DataStream and **NOT** interleaved on different refs (when multiple subs are active)
					s := d.subs[ref]
					l := len(d.subs[ref].buf)

					switch s.dataParts {
					case PartsUnknow:
						if l > 0 {
							d.log.Info(fmt.Sprintf("subs ref %d learning: is PartOne", ref))
							s.dataParts = PartsOne
							s.C <- s.buf
						}
						s.buf = append([]byte{}, p.Data...)
					case PartsOne:
						// immediate send
						s.C <- p.Data

					case PartsTwo:
						// save DATA
						if l > 0 {
							log.Panicf("buf len for subs ref %d error: want 0, got %d", ref, l)
						}
						s.buf = append([]byte{}, p.Data...)
					}

				case CodeDataStream2:
					// NOTE: assuming DataStream2 are sent adjacent to DataStream and **NOT** interleaved on different refs (when multiple subs are active)
					s := d.subs[ref]
					l := len(d.subs[ref].buf)

					switch s.dataParts {
					case PartsUnknow:
						if l > 0 {
							d.log.Info(fmt.Sprintf("subs ref %d learning: is PartTwo", ref))
							s.dataParts = PartsTwo
							s.C <- append(s.buf, p.Data...)
							s.buf = s.buf[:0]
						}
					case PartsOne:
						log.Panicf("subs ref %d is PartOne and received DATA_PART2", ref)

					case PartsTwo:
						s.C <- append(s.buf, p.Data...)
						s.buf = s.buf[:0]
					}
				}
			}
		}
	}
	return
}

// Send sends cmd
func (d *Device) Send(parent context.Context, cmd Command) (any, error) {

	ctx, cancel := context.WithTimeout(parent, SendCtxTimeout*time.Second)
	defer cancel()

	// assign ref
	var ref uint8
	if _, ok := cmd.(UnsubscribeCmd); ok {
		// Unsubscribe command holds ref in the command
		// **and* it does not reply with CommandReponse (#2): so just send it and return
		return nil, d.sendRaw(ctx, cmd)
	} else {
		ref = d.nextRef()
		cmd.SetRef(ref)
	}

	// create response chan
	ch := make(chan Packet, 1)
	d.muResp.Lock()
	d.resp[ref] = ch
	d.muResp.Unlock()

	// send cmd
	err := d.sendRaw(ctx, cmd)
	if err != nil {
		return nil, err
	}

	d.log.Debug(fmt.Sprintf("waiting from chan on ref=%d", ref))

	select {
	case rp := <-d.resp[ref]:
		cr, err := commandResponseFromPacket(rp)
		if err != nil {
			return nil, err
		}
		// remove resp after response received
		d.muResp.Lock()
		close(d.resp[ref])
		delete(d.resp, ref)
		d.muResp.Unlock()

		// check return code; skip for HelloCmd
		if _, ok := cmd.(HelloCmd); !ok {
			status := cr.getStatus()
			if status != StatusOK {
				return nil, fmt.Errorf("status failed. Want: %d, got: %d", StatusOK, status)
			}
		}
		// on successful command, update subscription map
		d.muSubs.Lock()
		switch c := cmd.(type) {
		case SubscribeCmd:
			d.subs[c.GetRef()] = NewSubscription(c.GetPath())
		case UnsubscribeCmd:
			close(d.subs[c.GetRef()].C)
			delete(d.subs, c.GetRef())
		}
		d.muSubs.Unlock()

		return cmd.Decode(*cr)

	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// GetSubs returns active Subcriptions
func (d *Device) GetSubs() Subscriptions {
	return d.subs
}

// GetSubsRef return Subscription to path, or error
func (d *Device) GetSubsRef(path string) (ref uint8, err error) {
	for k, v := range d.subs {
		if v.Path == path {
			return k, nil
		}
	}
	err = fmt.Errorf("no sub with path=%s", path)
	return
}

// Addr returns addr (BLE MAC) of client to connect to
func (d *Device) Addr() string {
	return d.addr
}

// Close closes
func (d *Device) Close() error {
	return d.transport.Close()
}

// nextRef finds next free ref
func (d *Device) nextRef() uint8 {
	d.muResp.Lock()
	defer d.muResp.Unlock()

	var initialRef = d.refCounter
	// if ref already in use, then continue to increment
	for {
		d.refCounter++
		// skip 0, don't wanna have that
		if d.refCounter == 0 {
			d.refCounter++
		}

		_, usedRecPac := d.resp[d.refCounter]
		_, usedSubscriptions := d.subs[d.refCounter]

		if !usedRecPac && !usedSubscriptions {
			return d.refCounter
		}
		if d.refCounter == initialRef {
			log.Panic("cannot allocate ref: no free ref available")
		}
	}
}

// sendSubDataIfComplete sends data if packet is complete
func (d *Device) sendSubDataIfComplete(dBuf []byte, ref uint8) (sent bool) {
	// FIXME: this is specific to IMU9, which sends n=4+(4*3)*k=4+12*n, with k proportional to Hz. Packet is complete when (n-4)%12==0
	if (len(dBuf)-4)%12 == 0 {
		select {
		case d.subs[ref].C <- dBuf:
			d.log.Debug(fmt.Sprintf("pushed data %v to channel subs[%v], which has size %d", dBuf, ref, len(d.subs)), "note", "device notify")
			return true
		default:
			log.Printf("cannot send to subs[%d] is possibly full", ref)
		}
	}
	return false
}

// sendRaw sends command and waits response
func (d *Device) sendRaw(ctx context.Context, cmd Command) (err error) {

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// send command
		sb := cmd.Encode()
		d.log.Debug(fmt.Sprintf("%+v", sb), "note", "sendRaw write bytes")
		if err = d.transport.Write(sb); err != nil {
			return
		}
	}
	return
}
