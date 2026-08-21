package udp

import (
	"encoding"
	"strings"

	"github.com/anacrolix/dht/v2/krpc"
	"github.com/anacrolix/torrent/tracker/shared"
	"github.com/anacrolix/torrent/types"
	"github.com/anacrolix/torrent/types/infohash"
)

// announceRequestWire is the BEP 15 82-byte UDP announce body. encoding/binary
// writes this layout. Keep it in field-for-field sync with AnnounceRequest
// through Port.
type announceRequestWire struct {
	InfoHash   infohash.T
	PeerId     types.PeerID
	Downloaded int64
	Left       int64
	Uploaded   int64
	Event      AnnounceEvent
	IPAddress  uint32
	Key        int32
	NumWant    int32
	Port       uint16
}

// Marshalled as binary by the UDP client, so be careful making changes.
type AnnounceRequest struct {
	InfoHash   infohash.T
	PeerId     types.PeerID
	Downloaded int64
	Left       int64 // If less than 0, math.MaxInt64 will be used for HTTP trackers instead.
	Uploaded   int64
	// Apparently this is optional. None can be used for announces done at
	// regular intervals.
	Event     AnnounceEvent
	IPAddress uint32
	Key       int32
	NumWant   int32 // How many peer addresses are desired. -1 for default.
	Port      uint16
	// Mariotte fork (BEP 21, Decent-Tako/Mariotte#161): caller-set partial-seed
	// flag. Not part of the 82-byte BEP 15 UDP body. HTTP encodes it as the
	// query parameter upload_only=1. UDP appends it as BEP 41 URLData after
	// that 82-byte body. This package does not decide when the flag is true.
	UploadOnly bool
} // 82-byte UDP body; UploadOnly is encoded separately

func (req AnnounceRequest) wire() announceRequestWire {
	return announceRequestWire{
		InfoHash:   req.InfoHash,
		PeerId:     req.PeerId,
		Downloaded: req.Downloaded,
		Left:       req.Left,
		Uploaded:   req.Uploaded,
		Event:      req.Event,
		IPAddress:  req.IPAddress,
		Key:        req.Key,
		NumWant:    req.NumWant,
		Port:       req.Port,
	}
}

func (wire announceRequestWire) toRequest() AnnounceRequest {
	return AnnounceRequest{
		InfoHash:   wire.InfoHash,
		PeerId:     wire.PeerId,
		Downloaded: wire.Downloaded,
		Left:       wire.Left,
		Uploaded:   wire.Uploaded,
		Event:      wire.Event,
		IPAddress:  wire.IPAddress,
		Key:        wire.Key,
		NumWant:    wire.NumWant,
		Port:       wire.Port,
	}
}

const uploadOnlyQuery = "upload_only=1"

func appendUploadOnlyQuery(requestUri string) string {
	sep := "?"
	if strings.Contains(requestUri, "?") {
		sep = "&"
	}
	return requestUri + sep + uploadOnlyQuery
}

// encodeAnnounceBody is the UDP announce payload after the 16-byte request
// header: the 82-byte BEP 15 body, then BEP 41 options (upload_only, then
// RequestUri).
func encodeAnnounceBody(req AnnounceRequest, opts Options) []byte {
	if req.UploadOnly {
		// BEP 21 / Mariotte#161: URLData is the existing extra-field position
		// on this packet. Do not place UploadOnly inside the 82-byte body.
		opts.RequestUri = appendUploadOnlyQuery(opts.RequestUri)
	}
	return append(mustMarshal(req), opts.Encode()...)
}

type AnnounceEvent = shared.AnnounceEvent

type AnnounceResponsePeers interface {
	encoding.BinaryUnmarshaler
	NodeAddrs() []krpc.NodeAddr
}
