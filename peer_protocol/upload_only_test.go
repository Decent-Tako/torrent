package peer_protocol

import (
	"strings"
	"testing"

	"github.com/anacrolix/torrent/bencode"
)

// BEP 21, "Extension Header": "A peer that is a partial seed SHOULD include an
// extra header in the extension handshake (specified in BEP 0010),
// 'upload_only'. Setting the value of this key to 1 indicates that this peer
// is not interested in downloading anything."
//
// The BEP's own example handshake is {'m': {'ut_metadata', 3}, 'upload_only': 1},
// so the key is a top-level integer 1, not a nested or boolean value.
// Decent-Tako/Mariotte#147.
func TestExtendedHandshakeCarriesUploadOnly(t *testing.T) {
	b, err := bencode.Marshal(ExtendedHandshakeMessage{UploadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, "11:upload_onlyi1e") {
		t.Fatalf("handshake %q lacks upload_only=1; a partial seed SHOULD send it (BEP 21)", got)
	}
}

// The key must be ABSENT, not zero, when we are not a partial seed. Sending
// upload_only=0 is a claim of its own on some clients, and it is not what
// BEP 21 describes.
func TestOrdinaryPeerOmitsUploadOnly(t *testing.T) {
	b, err := bencode.Marshal(ExtendedHandshakeMessage{})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); strings.Contains(got, "upload_only") {
		t.Fatalf("handshake %q sent upload_only while we still want bytes", got)
	}
}

// The value must survive a round trip, so a peer reading our handshake sees
// the claim we made.
func TestUploadOnlyRoundTrips(t *testing.T) {
	b, err := bencode.Marshal(ExtendedHandshakeMessage{UploadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	var out ExtendedHandshakeMessage
	if err := bencode.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !out.UploadOnly {
		t.Fatal("upload_only did not survive the round trip")
	}
}
