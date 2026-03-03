package gsp

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

var (
	macs   []string
	gsp    *GSP
	dev    map[string]*Device
	ctx    context.Context
	cancel context.CancelFunc
)

func TestMain(m *testing.M) {
	macsEnv := os.Getenv("MAC")
	if macsEnv == "" {
		os.Exit(0)
	}
	macs = strings.Split(macsEnv, ",")
	for i := range macs {
		macs[i] = strings.TrimSpace(macs[i])
	}

	// init
	dev = make(map[string]*Device)

	// context
	ctx, cancel = context.WithTimeout(context.Background(), 60*time.Second)

	// movesense
	gsp = New(ctx, slog.LevelError)
	if gsp == nil {
		log.Panic("cannot create GSP")
	}
	code := m.Run()
	cancel()
	for _, d := range dev {
		d.Close()
	}
	os.Exit(code)
}

func TestDevice(t *testing.T) {
	// add devices. No parallel execution, otherwise BLE controller fails with `can't dial: Command Disallowed`
	for _, mac := range macs {
		// add device
		t.Logf("Adding device %s", mac)
		err := gsp.AddDevice(mac)
		if err != nil {
			t.Fatalf("cannot add device %s: %v", mac, err)
		}
		dev[mac], err = gsp.GetDevice(mac)
		if err != nil {
			log.Panic(err)
		}
	}
	// run commands in parallel for each device, after connection
	for _, mac := range macs {
		t.Run(mac, func(t *testing.T) {
			t.Parallel()

			dev := dev[mac]
			// test commands
			// HELLO
			runHello(t, dev)

			// GET
			pathsGet := []string{
				"/Meas/IMU/Info",
				"/System/Energy",
			}
			for _, pathGet := range pathsGet {
				runGet(t, dev, pathGet)
			}

			// SUBSCRIBE
			pathsSubs := []string{
				"/Meas/IMU9/13", // only RespData
				//path := "/Meas/IMU9/416" // RespData + RespData2
			}
			for _, pathSubs := range pathsSubs {
				t.Run(pathSubs, func(t *testing.T) {
					t.Parallel()
					runSubs(t, dev, pathSubs, 2)
				})
			}
		})
	}
}

func runHello(t *testing.T, dev *Device) {
	cmd := NewHello()
	res, err := dev.Send(ctx, cmd)
	if err != nil {
		t.Fatalf("%s: HELLO send failed: %v", dev.Addr(), err)
	}
	t.Logf("%s: HELLO: %+v", dev.Addr(), res)
}

func runGet(t *testing.T, dev *Device, path string) {
	cmdGet := NewGet(path)
	res, err := dev.Send(ctx, cmdGet)
	if err != nil {
		t.Fatalf("GET %s failed: %v", path, err)
	}
	t.Logf("%s: GET path %s: %+v", dev.Addr(), path, res)
}

func runSubs(t *testing.T, dev *Device, path string, waitSec uint) {
	cmd := NewSubscribe(path)
	res, err := dev.Send(ctx, cmd)
	if err != nil {
		t.Fatalf("SUBSCRIBE failed: %v", err)
	}
	t.Logf("%s: SUBSCRIBE path %s: %+v", dev.Addr(), path, res)

	// get some subscribed data
	t.Logf("%s: Capturing %ds of subs data from path %s...", dev.Addr(), waitSec, path)

	ref, err := dev.GetSubsRef(path)
	if err != nil {
		log.Panic(err)
	}
	done := make(chan error, 1)
	go func() {
		subs := dev.GetSubs()

		timeout := time.NewTimer(time.Duration(waitSec) * time.Second)
		defer timeout.Stop()

		for {
			select {
			case d := <-subs[ref].C:
				t.Logf("%s: Received subs data from path=%s, ref=%d, len=%d: %v", dev.Addr(), path, ref, len(d), d)

			case <-timeout.C:
				ucmd := NewUnsubscribe(ref)
				res, err := dev.Send(ctx, ucmd)
				if err != nil {
					done <- fmt.Errorf("UNSUBSCRIBE failed: %v", err)
				}
				t.Logf("%s: UNSUBSCRIBE result: %+v", dev.Addr(), res)
				close(done)
				return
			}
		}
	}()
	if err := <-done; err != nil {
		t.Fatalf("subs faile: %v", err)
	}
}
