package gsp

import (
	"context"
	"log"
	"log/slog"
	"os"
	"testing"
	"time"
)

var (
	dev    *Device
	ctx    context.Context
	cancel context.CancelFunc
)

func TestMain(m *testing.M) {
	var err error
	mac := os.Getenv("MOVESENSE_MAC")
	if mac == "" {
		os.Exit(0) // skip all tests
	}

	// context
	ctx, cancel = context.WithTimeout(context.Background(), 60*time.Second)

	// movesense
	gsp := New(ctx, slog.LevelError)
	if gsp == nil {
		log.Panic("cannot create GSP")
	}
	log.Printf("Adding device %s", mac)
	err = gsp.AddDevice(mac)
	if err != nil {
		log.Panic("cannot add device ", mac)
	}
	dev, err = gsp.GetDevice(mac)
	if err != nil {
		log.Panic(err)
	}

	code := m.Run()

	dev.Close()
	cancel()
	os.Exit(code)
}

func TestHello(t *testing.T) {
	cmd := NewHello()

	res, err := dev.Send(ctx, cmd)
	if err != nil {
		t.Fatalf("HELLO send failed: %v", err)
	}

	t.Logf("result: %+v", res)
}

func TestCmd(t *testing.T) {
	apis := []string{
		"/Meas/IMU/Info",
		"/System/Energy",
	}

	for _, api := range apis {

		t.Run(api, func(t *testing.T) {
			cmdGet := NewGet(api)

			res, err := dev.Send(ctx, cmdGet)
			if err != nil {
				t.Fatalf("GET %s failed: %v", api, err)
			}

			t.Logf("result: %+v", res)
		})
	}
}

func TestSubsIMU9(t *testing.T) {
	// test parallel (interleaved) subs, see TestSubscribeAcc
	t.Parallel()

	path := "/Meas/IMU9/13" // only RespData
	//path := "/Meas/IMU9/416" // RespData + RespData2

	t.Log("Subscribing to:", path)

	cmd := NewSubscribe(path)

	res, err := dev.Send(ctx, cmd)
	if err != nil {
		t.Fatalf("SUBSCRIBE failed: %v", err)
	}

	t.Logf("result: %+v", res)

	// get some subscribed data
	waitSec := 3
	t.Logf("Capture %ds of subs data...", waitSec)

	ref, err := dev.GetSubsRef(path)
	if err != nil {
		log.Panic(err)
	}
	go func() {
		subs := dev.GetSubs()

		timeout := time.NewTimer(time.Duration(waitSec) * time.Second)
		defer timeout.Stop()

	For:
		for {
			select {
			case d := <-subs[ref].C:
				t.Logf("Received subs data from ref %d, len=%d: %v", ref, len(d), d)

			case <-timeout.C:
				ucmd := NewUnsubscribe(ref)
				res, err := dev.Send(ctx, ucmd)
				if err != nil {
					log.Printf("UNSUBSCRIBE failed: %v", err)
					break For
				}
				t.Logf("UNSUBSCRIBE result: %+v", res)

				break For
			}
		}
	}()
	time.Sleep(time.Duration(2*waitSec) * time.Second)
}

func TestSubsAcc(t *testing.T) {
	t.Parallel()

	path := "/Meas/Acc/13"

	t.Log("Subscribing to:", path)

	cmd := NewSubscribe(path)

	res, err := dev.Send(ctx, cmd)
	if err != nil {
		t.Fatalf("SUBSCRIBE failed: %v", err)
	}

	t.Logf("result: %+v", res)

	// get some subscribed data
	waitSec := 3
	t.Logf("Capture %ds of subscrubed data...", waitSec)

	ref, err := dev.GetSubsRef(path)
	if err != nil {
		log.Panic(err)
	}
	go func() {
		subs := dev.GetSubs()

		timeout := time.NewTimer(time.Duration(waitSec) * time.Second)
		defer timeout.Stop()

	For:
		for {
			select {
			case d := <-subs[ref].C:
				t.Logf("Received subs data from ref %d, len=%d: %v", ref, len(d), d)

			case <-timeout.C:
				ucmd := NewUnsubscribe(ref)
				res, err := dev.Send(ctx, ucmd)
				if err != nil {
					log.Printf("UNSUBSCRIBE failed: %v", err)
					break For
				}
				t.Logf("UNSUBSCRIBE result: %+v", res)

				break For
			}
		}
	}()
	time.Sleep(time.Duration(2*waitSec) * time.Second)
}
