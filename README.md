## Description

Independent Go implementation of the Movesense [GSP][] protocol, implementing host (BLE central) functions to communicate with [Movesense HR+][HR+] via [GSP][].

Tested on `linux/amd64` and `linux/arm` hosts, connecting to [HR+][] devices with **firmware `v2.3.2`**.

It relies on [go-ble](<github.com/go-ble/ble>), which requires root privileges.

The following commands are implemented: `HELLO`, `GET`, `SUBSCRIBE`, `UNSUBSCRIBE`, `PUT_UTCTIME` based on [GSP][], plus `Device` helpers `PutUTCTime`, `DetailedTime` (`/Time/Detailed`: relative ms to UTC mapping) and `EnergyLevel` (`/System/Energy/Level`: battery %). API are documented in [Movesense API][MAPI] and more details can be found in [Swagger definitions][MAPI-src]. Interleaved subscribed command are supported.

---

## Test and sample

See: [command_test.go](./gsp/command_test.go), which can be run via:

1. wake up the device (e.g. touch terminals with two fingers of one hand)
2. `go test -c -o test ./gsp && sudo MAC=<MY_HR_MAC>[,<MAC2>...] HCI=hci0 ./test -test.v`

### Subscribe sample

To receive data from a subscription:

1. `gsp.New(ctx, level, "hci0")`, then `AddDevice(mac)` / `GetDevice(mac)`
2. issue a successful `gsp.NewSubscribe(path)` command on the device
3. receive binary data from relevant ref channel: `<- d.GetSubs()[d.GetSubsRef(path)]`

See sample:
```go
        hz := 416
        // connect
        ms := gsp.New(ctx, slog.LevelError, "hci0")
        if err := ms.AddDevice(mac); err != nil {
                log.Printf("Failed to connect to %s: %v", mac, err)
                return
        }
        d, err := ms.GetDevice(mac)
        if err != nil {
                log.Panicf("cannot get device %s: %v", mac, err)
        }
        log.Printf("Connected to %v", mac)
        // subscribe
        path := fmt.Sprintf("/Meas/IMU9/%d", hz)
        cmd := gsp.NewSubscribe(path)
        _, err = d.Send(ctx, cmd)
        if err != nil {
                log.Panicf("subscribe to %s failed: %v", path, err)
        }
        // receive data
        ref, err := d.GetSubsRef(path)
        if err != nil {
                log.Panicf("cannot get ref for %s: %v", path, err)
        }
        subs := d.GetSubs()
        for {
                data := <-subs[ref].C
                // USE data
                // ...
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
