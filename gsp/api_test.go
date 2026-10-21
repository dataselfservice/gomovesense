package gsp

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestPutUTCTimeEncode(t *testing.T) {
	ts := time.UnixMicro(1761072000123456)
	b := NewPutUTCTime(ts).Encode()
	if b[0] != CmdPutUTCTime || len(b) != 10 || binary.LittleEndian.Uint64(b[2:]) != uint64(ts.UnixMicro()) {
		t.Fatalf("encode: %v", b)
	}
}

func TestDecodeDetailedTime(t *testing.T) {
	b := binary.LittleEndian.AppendUint64(nil, 1761072000000000)
	b = binary.LittleEndian.AppendUint32(b, 5000)
	b = binary.LittleEndian.AppendUint32(b, 32768)
	dt, err := decodeDetailedTime(b)
	if err != nil {
		t.Fatal(err)
	}
	if dt.RelativeMs != 5000 || dt.TickRate != 32768 || !dt.UTC.Equal(time.UnixMicro(1761072000000000)) {
		t.Fatalf("decode: %+v", dt)
	}
	if got := dt.At(5250); !got.Equal(dt.UTC.Add(250 * time.Millisecond)) {
		t.Fatalf("At: %v", got)
	}
	if got := dt.At(4000); !got.Equal(dt.UTC.Add(-time.Second)) {
		t.Fatalf("At before sync: %v", got)
	}
	if _, err := decodeDetailedTime(b[:15]); err == nil {
		t.Fatal("short payload: want error")
	}
}
