package gsp

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	CodeCommandResponse = 1
	CodeDataStream      = 2
	CodeDataStream2     = 3

	RespCommand = 0x01
	RespData    = 0x02
	RespData2   = 0x03
)

// Packet packet data, see: https://www.movesense.com/docs/esw/gatt_sensordata_protocol/#command-packet-format-all-commands
type Packet struct {
	Code uint8
	Ref  uint8
	Data []byte
}

func NewPacket() *Packet {
	return &Packet{Data: make([]byte, 256)}
}

// NewPacketFromBytes return Packet from bytes, or error
func NewPacketFromBytes(b []byte) (p *Packet, err error) {
	if len(b) < 2 {
		return nil, errors.New("short packet")
	}
	return &Packet{
		Code: b[0],
		Ref:  b[1],
		Data: b[2:],
	}, nil
}

func (p *Packet) SetRef(ref uint8) {
	p.Ref = ref
}
func (p *Packet) GetRef() uint8 {
	return p.Ref
}
func (p *Packet) Encode() []byte {
	b := make([]byte, 2+len(p.Data))
	b[0] = p.Code
	b[1] = p.Ref
	copy(b[2:], p.Data)
	return b
}

// Command interface
type Command interface {
	// SetRef sets Ref
	SetRef(uint8)
	// GetRef gets Ref
	GetRef() uint8
	// Encode to binary
	Encode() []byte
	// Decode decodes CommandResponse
	Decode(CommandResponse) (any, error)
}

func NewCommand(commandCode uint8, data []byte) *Packet {
	return &Packet{
		Code: commandCode,
		Data: data,
	}
}

// CommandResponse resp to commands
type CommandResponse Packet

func commandResponseFromPacket(p Packet) (cr *CommandResponse, err error) {
	if p.Code != CodeCommandResponse {
		return nil, fmt.Errorf("invalid packet code for CommandResponse: want %d, got %d", CodeCommandResponse, p.Code)
	}
	cr = (*CommandResponse)(&p)
	return
}

// getStatus returns Status
func (cr *CommandResponse) getStatus() uint16 {
	return binary.LittleEndian.Uint16(cr.Data)
}

// SubsDataStream data notified in when a subscription is active
type SubsDataStream Packet

func SubsDataStreamDecode(b []byte) (d *SubsDataStream, err error) {
	p, err := NewPacketFromBytes(b)
	if p.Code != CodeDataStream {
		return nil, fmt.Errorf("invalid packet code for DataStream: want %d, got %d", CodeDataStream, p.Code)
	}
	d = (*SubsDataStream)(p)
	return
}

// SubsDataStream2 second part of data notified when a subscription is active
type SubsDataStream2 Packet

func SubsDataStream2Decode(b []byte) (d2 *SubsDataStream2, err error) {
	p, err := NewPacketFromBytes(b)
	if p.Code != CodeDataStream {
		return nil, fmt.Errorf("invalid packet code for DataStream2: want %d, got %d", CodeDataStream, p.Code)
	}
	d2 = (*SubsDataStream2)(p)
	return
}
