package gsp

import (
	"context"
	"errors"
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

// Subscription to receive data subscribed to Path via C
type Subscription struct {
	// Path path subscribed
	Path string
	// C chan to receive data
	C chan []byte
}

// Subscriptions map of active subscriptions
type Subscriptions map[uint8]Subscription

// GSP main GSP struct to interact with BLE client device
type GSP struct {
	addr string

	log *slog.Logger

	transport  BLETransport
	refCounter uint8

	resp   map[uint8]chan Packet
	muResp sync.Mutex

	subs   Subscriptions
	muSubs sync.Mutex
}

func New(addr string, logLevel slog.Level) *GSP {
	return &GSP{
		addr: addr,
		log: slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level:     logLevel,
			AddSource: true,
		})).With("MAC", addr),
	}
}

// Connect to BLE client device having d.addr
func (d *GSP) Connect(ctx context.Context) (err error) {
	// reset
	d.resp = make(map[uint8]chan Packet)
	d.subs = make(Subscriptions)

	// BLE
	dev, err := linux.NewDevice()
	if err != nil {
		return
	}
	ble.SetDefaultDevice(dev)

	addr := ble.NewAddr(d.addr)

	client, err := ble.Dial(ctx, addr)
	if err != nil {
		return
	}

	// tune MTU
	_, err = client.ExchangeMTU(BleMTU)
	if err != nil {
		return
	}

	// discover and get desired characteristics
	profile, err := client.DiscoverProfile(true)
	if err != nil {
		cerr := client.CancelConnection()
		if cerr != nil {
			log.Print(cerr)
		}
		return
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
		cerr := client.CancelConnection()
		if cerr != nil {
			log.Print(cerr)
		}
		return errors.New("GSP characteristics not found")
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
		cerr := client.CancelConnection()
		if cerr != nil {
			log.Print(cerr)
		}
		return
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
	go func() {

		dBuf := []byte{}
		for raw := range d.transport.Notify() {
			// TODO: eventually move to clean shutdown, and turn to for { select {} }

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
				// NOTE: assuming DataStream2 are send adjacent to DataStream and **NOT** interleaved on differente refs (when multiple subs are active)
				// reseting
				if len(dBuf) > 0 {
					log.Panicf("subs buffer len not zero: want 0, got %d", len(dBuf))
				}
				dBuf = append(dBuf, p.Data...)
				if d.sendSubDataIfComplete(dBuf, ref) {
					dBuf = dBuf[:0]
				}

			case CodeDataStream2:
				dBuf = append(dBuf, p.Data...)
				if d.sendSubDataIfComplete(dBuf, ref) {
					dBuf = dBuf[:0]
				}
			}

		}
		log.Print("chan closed")

	}()

	return
}

// Send sends cmd
func (d *GSP) Send(parent context.Context, cmd Command) (any, error) {

	ctx, cancel := context.WithTimeout(parent, SendCtxTimeout*time.Second)
	defer cancel()

	// assign ref
	var ref uint8
	if _, ok := cmd.(UnsubscribeCmd); ok {
		// Unsubscribe command holds ref in the command
		ref = cmd.GetRef()
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
			d.subs[c.GetRef()] = Subscription{Path: c.GetPath(), C: make(chan []byte, 1)}
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
func (d *GSP) GetSubs() Subscriptions {
	return d.subs
}

// GetSubsRef return Subscription to path, or error
func (d *GSP) GetSubsRef(path string) (ref uint8, err error) {
	for k, v := range d.subs {
		if v.Path == path {
			return k, nil
		}
	}
	err = fmt.Errorf("no sub with path=%s", path)
	return
}

// Addr returns addr (BLE MAC) of client to connect to
func (d *GSP) Addr() string {
	return d.addr
}

// Close closes
func (d *GSP) Close() error {
	return d.transport.Close()
}

// nextRef finds next free ref
func (d *GSP) nextRef() uint8 {
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
func (d *GSP) sendSubDataIfComplete(dBuf []byte, ref uint8) (sent bool) {
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
func (d *GSP) sendRaw(ctx context.Context, cmd Command) (err error) {

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
