package gsp

const (
	CmdHello       = 0
	CmdSubscribe   = 1
	CmdUnsubscribe = 2
	CmdGet         = 4
)

// HelloCmd see: https://www.movesense.com/docs/esw/gatt_sensordata_protocol/#hello
type HelloCmd struct {
	*Packet
}

func NewHello() HelloCmd { return HelloCmd{NewCommand(CmdHello, []byte{})} }

type HelloResult struct {
	Protocol uint8
	Serial   string
	Product  string
	DFUMac   string
	AppName  string
	AppVer   string
}

func (c HelloCmd) Decode(cr CommandResponse) (any, error) {
	d := cr.Data
	i := 1

	readStr := func() string {
		start := i
		for d[i] != 0 {
			i++
		}
		s := string(d[start:i])
		i++
		return s
	}

	return HelloResult{
		Protocol: d[0],
		Serial:   readStr(),
		Product:  readStr(),
		DFUMac:   readStr(),
		AppName:  readStr(),
		AppVer:   readStr(),
	}, nil
}

// GetCmd see: https://www.movesense.com/docs/esw/gatt_sensordata_protocol/#get
type GetCmd struct {
	*Packet
}

func NewGet(path string) GetCmd {
	return GetCmd{NewCommand(CmdGet, []byte(path))}
}
func (c GetCmd) Decode(cr CommandResponse) (any, error) {
	return cr.Data, nil
}

// SubscribeCmd see: https://www.movesense.com/docs/esw/gatt_sensordata_protocol/#subscribe
type SubscribeCmd struct {
	*Packet
}

func NewSubscribe(path string) SubscribeCmd {
	return SubscribeCmd{NewCommand(CmdSubscribe, []byte(path))}
}
func (c SubscribeCmd) Decode(cr CommandResponse) (any, error) {
	return cr.Data, nil
}
func (c SubscribeCmd) GetPath() string {
	return string(c.Data)
}

// UnsubscribeCmd see: https://www.movesense.com/docs/esw/gatt_sensordata_protocol/#unsubscribe
type UnsubscribeCmd struct {
	*Packet
}

func NewUnsubscribe(ref uint8) (uc UnsubscribeCmd) {
	uc = UnsubscribeCmd{NewCommand(CmdUnsubscribe, []byte{})}
	uc.Ref = ref
	return
}
func (c UnsubscribeCmd) Decode(cr CommandResponse) (any, error) {
	return cr.Data, nil
}
