package udp

import (
	"bytes"
	"encoding/binary"
	"testing"

	qt "github.com/go-quicktest/qt"
)

// BEP 21 / Decent-Tako/Mariotte#161: UploadOnly must not change the 82-byte
// BEP 15 body. It is encoded as BEP 41 URLData after that body. This fails if
// the field is dropped from the encoded announce or folded into the body.
func TestAnnounceRequestUploadOnlyWire(t *testing.T) {
	qt.Assert(t, qt.Equals(binary.Size(announceRequestWire{}), 82))

	req := AnnounceRequest{
		Port:       6881,
		NumWant:    -1,
		UploadOnly: true,
	}
	var body bytes.Buffer
	qt.Assert(t, qt.IsNil(Write(&body, req)))
	qt.Assert(t, qt.Equals(body.Len(), 82))

	encoded := encodeAnnounceBody(req, Options{RequestUri: "/announce"})
	qt.Assert(t, qt.DeepEquals(encoded[:82], body.Bytes()))
	wantURI := "/announce?upload_only=1"
	wantOpt := append([]byte{optionTypeURLData, byte(len(wantURI))}, wantURI...)
	qt.Assert(t, qt.DeepEquals(encoded[82:], wantOpt))

	off := encodeAnnounceBody(AnnounceRequest{Port: 6881}, Options{RequestUri: "/announce"})
	qt.Assert(t, qt.DeepEquals(off[82:], append([]byte{optionTypeURLData, 9}, "/announce"...)))
}

func TestAnnounceRequestWriteReadKeepsBodyWithoutUploadOnly(t *testing.T) {
	in := AnnounceRequest{Port: 6881, Key: 7, UploadOnly: true, Event: 2}
	var buf bytes.Buffer
	qt.Assert(t, qt.IsNil(Write(&buf, in)))
	var out AnnounceRequest
	qt.Assert(t, qt.IsNil(Read(&buf, &out)))
	qt.Assert(t, qt.Equals(out.Port, in.Port))
	qt.Assert(t, qt.Equals(out.Key, in.Key))
	qt.Assert(t, qt.Equals(out.Event, in.Event))
	qt.Assert(t, qt.Equals(out.UploadOnly, false))
}
