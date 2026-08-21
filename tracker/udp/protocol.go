package udp

import (
	"bytes"
	"encoding/binary"
	"io"
)

type Action int32

const (
	ActionConnect Action = iota
	ActionAnnounce
	ActionScrape
	ActionError
)

const ConnectRequestConnectionId = 0x41727101980

const (
	// BEP 41
	optionTypeEndOfOptions = 0
	optionTypeNOP          = 1
	optionTypeURLData      = 2
)

type TransactionId = int32

type ConnectionId = uint64

type ConnectionRequest struct {
	ConnectionId  ConnectionId
	Action        Action
	TransactionId TransactionId
}

type ConnectionResponse struct {
	ConnectionId ConnectionId
}

type ResponseHeader struct {
	Action        Action
	TransactionId TransactionId
}

type RequestHeader struct {
	ConnectionId  ConnectionId
	Action        Action
	TransactionId TransactionId
} // 16 bytes

type AnnounceResponseHeader struct {
	Interval int32
	Leechers int32
	Seeders  int32
}

type InfoHash = [20]byte

func marshal(data interface{}) (b []byte, err error) {
	var buf bytes.Buffer
	err = Write(&buf, data)
	b = buf.Bytes()
	return
}

func mustMarshal(data interface{}) []byte {
	b, err := marshal(data)
	if err != nil {
		panic(err)
	}
	return b
}

// This is for fixed-size, builtin types only I think.
// AnnounceRequest is special-cased: UploadOnly is not in the 82-byte BEP 15
// body (BEP 21 / Decent-Tako/Mariotte#161).
func Write(w io.Writer, data interface{}) error {
	switch v := data.(type) {
	case AnnounceRequest:
		return binary.Write(w, binary.BigEndian, v.wire())
	case *AnnounceRequest:
		return binary.Write(w, binary.BigEndian, v.wire())
	default:
		return binary.Write(w, binary.BigEndian, data)
	}
}

func Read(r io.Reader, data interface{}) error {
	if ar, ok := data.(*AnnounceRequest); ok {
		var wire announceRequestWire
		if err := binary.Read(r, binary.BigEndian, &wire); err != nil {
			return err
		}
		*ar = wire.toRequest()
		return nil
	}
	return binary.Read(r, binary.BigEndian, data)
}
