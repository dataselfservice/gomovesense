## Description

Independent Go implementation of the Movesense [GSP][] protocol, implementing host (BLE central) functions to communicate with [Movesense HR+][HR+] via [GSP][].

Tested on `linux/amd64` and `linux/arm` hosts, connecting to [HR+][] devices with **firmware `v2.3.2`**.

It relies on [go-ble](<github.com/go-ble/ble>), which requires root privileges.

The following commands are implemented: `HELLO`, `GET`, `SUBSCRIBE`, `UNSUBSCRIBE` based on [GSP][]. API are documented in [Movesense API][MAPI] and more details can be found in [Swagger definitions][MAPI-src]. Interleaved subscribed command are supported.

---

## Test and sample

See: [command_test.go](./gsp/command_test.go), which can be run via:

1. wake up the device (e.g. touch terminals with two fingers of one hand)
2. `go test -c -o test ./gsp && sudo MOVESENSE_MAC=<MY_HR_MAC> ./test -test.v`

### Subscribe sample

To receive data from a subscription:

1. issue a successful `gsp.NewSubcribe(path)` command
2. receive binary data from relevant ref channel: `<- GetSubs()[GetSubsRef(path)]`

See sample:
```go
        hz:=416
        // connect
        ms := gsp.New(mac, slog.LevelError)
        if err := ms.Connect(ctx); err != nil {
                log.Printf("Failed to connect to %s failed: %v", mac, err)
                return
        }
        log.Printf("Connected to %v", mac)
        // subscribe
        path := fmt.Sprintf("/Meas/IMU9/%d", hz)
        cmd := gsp.NewSubscribe(path)
        _, err = ms.Send(ctx, cmd)
        if err != nil {
                log.Panicf("subscribe to %s failed: %v", path, err)
        }
        // receive data
        ref, err := ms.GetSubsRef(path)
        if err != nil {
                log.Panicf("cannot get ref for %s: %v", path, err)
        }
        subs := ms.GetSubs()
        for {
                data := <-subs[ref].C
                // USE data
                // ...
                }
        }
```

---

## Notes

* "Movesense" is a trademark of [Movesense Ltd][MS]
* This project is not affiliated with or endorsed by [Movesense][MS]

---

## License

This project is licensed under the Apache License 2.0 - see the LICENSE file for details.

---




[GSP]: https://movesense.com/docs/esw/gatt_sensordata_protocol/
[MDL]: https://bitbucket.org/movesense/movesense-device-lib.git
[MAPI]: https://movesense.com/docs/esw/api_reference/
[MAPI-src]: https://bitbucket.org/movesense/movesense-device-lib/src/release_2.3.1/MovesenseCoreLib/resources/movesense-api/
[HR+]: https://www.movesense.com/product/movesense-sensor-hr/
[MS]: https://www.movesense.com/
