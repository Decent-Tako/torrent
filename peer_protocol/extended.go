package peer_protocol

import (
	"net"
)

// http://www.bittorrent.org/beps/bep_0010.html
type (
	ExtendedHandshakeMessage struct {
		M    map[ExtensionName]ExtensionNumber `bencode:"m"`
		V    string                            `bencode:"v,omitempty"`
		Reqq int                               `bencode:"reqq,omitempty"`
		// The only mention of this I can find is in https://www.bittorrent.org/beps/bep_0011.html
		// for bit 0x01.
		Encryption bool `bencode:"e"`
		// BEP 9
		MetadataSize int `bencode:"metadata_size,omitempty"`
		// The local client port. It would be redundant for the receiving side of
		// a connection to send this.
		Port   int       `bencode:"p,omitempty"`
		YourIp CompactIp `bencode:"yourip,omitempty"`
		Ipv4   CompactIp `bencode:"ipv4,omitempty"`
		Ipv6   net.IP    `bencode:"ipv6,omitempty"`
		// Mariotte fork (BEP 21, Decent-Tako/Mariotte#147). BEP 21: "A peer
		// that is a partial seed SHOULD include an extra header in the
		// extension handshake (specified in BEP 0010), 'upload_only'. Setting
		// the value of this key to 1 indicates that this peer is not
		// interested in downloading anything."
		//
		// omitempty is required, not cosmetic: the key must be absent, not 0,
		// when we are not a partial seed. This package does not decide the
		// value.
		UploadOnly bool `bencode:"upload_only,omitempty"`
	}

	ExtensionName   string
	ExtensionNumber uint8
)

const (
	// http://www.bittorrent.org/beps/bep_0011.html
	ExtensionNamePex ExtensionName = "ut_pex"

	ExtensionDeleteNumber ExtensionNumber = 0
)

func (me *ExtensionNumber) UnmarshalBinary(b []byte) error {
	*me = ExtensionNumber(b[0])
	return nil
}
