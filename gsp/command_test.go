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
	ms     *GSP
	ctx    context.Context
	cancel context.CancelFunc
)

func TestMain(m *testing.M) {
	mac := os.Getenv("MOVESENSE_MAC")
	if mac == "" {
		os.Exit(0) // skip all tests
	}

	// context
	ctx, cancel = context.WithTimeout(context.Background(), 60*time.Second)

	// movesense
	ms = New(mac, slog.LevelError)
	if err := ms.Connect(ctx); err != nil {
		panic(err)
	}

	code := m.Run()

	ms.Close()
	cancel()
	os.Exit(code)
}

func TestHello(t *testing.T) {
	cmd := NewHello()

	res, err := ms.Send(ctx, cmd)
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

			res, err := ms.Send(ctx, cmdGet)
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

	res, err := ms.Send(ctx, cmd)
	if err != nil {
		t.Fatalf("SUBSCRIBE failed: %v", err)
	}

	t.Logf("result: %+v", res)

	// get some subscribed data
	waitSec := 3
	t.Logf("Capture %ds of subs data...", waitSec)

	ref, err := ms.GetSubsRef(path)
	if err != nil {
		log.Panic(err)
	}
	go func() {
		subs := ms.GetSubs()

		timeout := time.NewTimer(time.Duration(waitSec) * time.Second)
		defer timeout.Stop()

	For:
		for {
			select {
			case d := <-subs[ref].C:
				t.Logf("Received subs data from ref %d, len=%d: %v", ref, len(d), d)

			case <-timeout.C:
				ucmd := NewUnsubscribe(ref)
				res, err := ms.Send(ctx, ucmd)
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

	res, err := ms.Send(ctx, cmd)
	if err != nil {
		t.Fatalf("SUBSCRIBE failed: %v", err)
	}

	t.Logf("result: %+v", res)

	// get some subscribed data
	waitSec := 3
	t.Logf("Capture %ds of subscrubed data...", waitSec)

	ref, err := ms.GetSubsRef(path)
	if err != nil {
		log.Panic(err)
	}
	go func() {
		subs := ms.GetSubs()

		timeout := time.NewTimer(time.Duration(waitSec) * time.Second)
		defer timeout.Stop()

	For:
		for {
			select {
			case d := <-subs[ref].C:
				t.Logf("Received subs data from ref %d, len=%d: %v", ref, len(d), d)

			case <-timeout.C:
				ucmd := NewUnsubscribe(ref)
				res, err := ms.Send(ctx, ucmd)
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
