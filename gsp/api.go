package gsp

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"
)

const (
	PathTimeDetailed = "/Time/Detailed"
	PathEnergyLevel  = "/System/Energy/Level"
)

// DetailedTime maps the sensor relative clock (ms since reset, as in IMU timestamps) to UTC
type DetailedTime struct {
	UTC        time.Time
	RelativeMs uint32
	TickRate   uint32
}

// At returns the UTC time of relative timestamp ms
func (t DetailedTime) At(ms uint32) time.Time {
	return t.UTC.Add(time.Duration(int64(ms)-int64(t.RelativeMs)) * time.Millisecond)
}

// getData sends GET path and returns the payload (GSP binary format, status stripped)
func (d *Device) getData(ctx context.Context, path string) ([]byte, error) {
	res, err := d.Send(ctx, NewGet(path))
	if err != nil {
		return nil, err
	}
	b, _ := res.([]byte)
	if len(b) < 2 {
		return nil, fmt.Errorf("GET %s: short response %v", path, b)
	}
	return b[2:], nil
}

// PutUTCTime sets the sensor UTC clock
func (d *Device) PutUTCTime(ctx context.Context, t time.Time) error {
	_, err := d.Send(ctx, NewPutUTCTime(t))
	return err
}

// DetailedTime gets UTC and relative time at the same instant
func (d *Device) DetailedTime(ctx context.Context) (DetailedTime, error) {
	b, err := d.getData(ctx, PathTimeDetailed)
	if err != nil {
		return DetailedTime{}, err
	}
	return decodeDetailedTime(b)
}

func decodeDetailedTime(b []byte) (DetailedTime, error) {
	// utcTime int64 us, relativeTime uint32 ms, tickRate uint32, [accuracy uint32]
	if len(b) < 16 {
		return DetailedTime{}, fmt.Errorf("GET %s: short payload %v", PathTimeDetailed, b)
	}
	return DetailedTime{
		UTC:        time.UnixMicro(int64(binary.LittleEndian.Uint64(b))).UTC(),
		RelativeMs: binary.LittleEndian.Uint32(b[8:]),
		TickRate:   binary.LittleEndian.Uint32(b[12:]),
	}, nil
}

// EnergyLevel gets the battery charge in percent
func (d *Device) EnergyLevel(ctx context.Context) (uint8, error) {
	b, err := d.getData(ctx, PathEnergyLevel)
	if err != nil {
		return 0, err
	}
	if len(b) < 1 {
		return 0, fmt.Errorf("GET %s: empty payload", PathEnergyLevel)
	}
	return b[0], nil
}
