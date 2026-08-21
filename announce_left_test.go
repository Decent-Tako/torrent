package torrent

import (
	"os"
	"testing"

	"github.com/anacrolix/torrent/internal/testutil"
	"github.com/anacrolix/torrent/storage"
)

// leftTestTorrent builds a torrent with info set, so bytesLeftAnnounce
// computes a real Left rather than returning -1.
func leftTestTorrent(t *testing.T, cfg *ClientConfig) *Torrent {
	t.Helper()
	dir, mi := testutil.GreetingTestTorrent()
	t.Cleanup(func() { os.RemoveAll(dir) })
	var cl Client
	cfg.DefaultStorage = storage.NewFileWithCompletion(cfg.DataDir, storage.NewMapPieceCompletion())
	cl.init(cfg)
	t.Cleanup(func() {
		for _, f := range cl.onClose {
			f()
		}
	})
	tor := cl.newTorrent(
		mi.HashInfoBytes(),
		storage.NewFileWithCompletion(t.TempDir(), storage.NewMapPieceCompletion()),
	)
	cl.lock()
	err := tor.setInfoBytesLocked(mi.InfoBytes)
	cl.unlock()
	if err != nil {
		t.Fatal(err)
	}
	if !tor.haveInfo() {
		t.Fatal("info not set")
	}
	return tor
}

// Decent-Tako/Mariotte#147. bytesLeft comes from the completed-pieces bitmap,
// which Mariotte's storage-backed completion overlay fills for pieces whose
// only copy is in object storage. Left is what tells a tracker whether we are a
// seed, so taking it from that bitmap announces a FULL SEED while the node
// holds none of the bytes. §17.1 forbids exactly that: "Do not use REMOTE_ONLY
// completion to force Left=0."
//
// The hook reports what the node actually holds, and the announce uses it.
func TestAnnounceBytesLeftOverridesCompletion(t *testing.T) {
	cfg := TestingConfig(t)
	// Hold exactly one byte locally, whatever the fixture's size, so the
	// expected Left is unambiguous and never negative.
	const held = int64(1)
	cfg.AnnounceBytesLeft = func(_ [20]byte, total int64) (int64, bool) {
		return total - held, true
	}
	tor := leftTestTorrent(t, cfg)

	want := tor.length() - held
	if got := tor.bytesLeftAnnounce(); got != want {
		t.Fatalf("bytesLeftAnnounce=%d want %d: Left must report bytes actually held, "+
			"not the completion bitmap (§17.1)", got, want)
	}
}

// The case the reviewer flagged: everything "complete" through the overlay,
// nothing held locally. Left must NOT be 0.
func TestAnnounceBytesLeftIsNotZeroWhenNothingIsHeldLocally(t *testing.T) {
	cfg := TestingConfig(t)
	cfg.AnnounceBytesLeft = func(_ [20]byte, total int64) (int64, bool) {
		// Cold seed: every piece is in object storage, none held locally.
		return total, true
	}
	tor := leftTestTorrent(t, cfg)

	got := tor.bytesLeftAnnounce()
	if got == 0 {
		t.Fatal("announced Left=0 while holding no local bytes; that claims full-seed status (§17.1)")
	}
	if got != tor.length() {
		t.Fatalf("bytesLeftAnnounce=%d want %d (nothing held locally)", got, tor.length())
	}
}

// ok=false means "unknown": keep the library's own accounting rather than
// announcing a number Mariotte could not compute.
func TestAnnounceBytesLeftFalseFallsBack(t *testing.T) {
	cfg := TestingConfig(t)
	cfg.AnnounceBytesLeft = func([20]byte, int64) (int64, bool) {
		return 999999, false
	}
	tor := leftTestTorrent(t, cfg)
	if got := tor.bytesLeftAnnounce(); got == 999999 {
		t.Fatal("used the hook's value despite ok=false")
	}
}

// A nil hook must leave stock behaviour untouched. This fork is vendored, so a
// change to Left for non-Mariotte callers would be a silent protocol change.
func TestAnnounceBytesLeftNilKeepsStockBehaviour(t *testing.T) {
	cfg := TestingConfig(t)
	if cfg.AnnounceBytesLeft != nil {
		t.Fatal("AnnounceBytesLeft must default to nil so stock Left is unchanged")
	}
	tor := leftTestTorrent(t, cfg)
	if got, want := tor.bytesLeftAnnounce(), tor.bytesLeft(); got != want {
		t.Fatalf("bytesLeftAnnounce=%d want the stock bytesLeft %d", got, want)
	}
}
