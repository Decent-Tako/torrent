package torrent

import (
	"io"

	"github.com/anacrolix/missinggo/v2/panicif"

	"github.com/anacrolix/torrent/storage"
)

func (t *Torrent) storageReader() storageReader {
	if t.storage.NewReader == nil {
		return &storagePieceReader{t: t}
	}
	return torrentStorageImplReader{
		implReader: t.storage.NewReader(),
		t:          t,
	}
}

// This wraps per-piece storage as a whole-torrent storageReader.
type storagePieceReader struct {
	t       *Torrent
	pr      storage.PieceReader
	prIndex int
}

func (me *storagePieceReader) Close() (err error) {
	if me.pr != nil {
		err = me.pr.Close()
	}
	return
}

func (me *storagePieceReader) getReaderAt(p Piece) (err error) {
	if me.pr != nil {
		if me.prIndex == p.Index() {
			return
		}
		panicif.Err(me.pr.Close())
		me.pr = nil
	}
	ps := p.Storage()
	me.prIndex = p.Index()
	me.pr, err = ps.NewReader()
	return
}

func (me *storagePieceReader) ReadAt(b []byte, off int64) (n int, err error) {
	return me.readAt(b, off, false)
}

// ReadAtPeerServe implements PeerServeReaderAt: the bytes are destined for a
// remote peer, so a backend may refuse rather than fetch them inline (#260).
func (me *storagePieceReader) ReadAtPeerServe(b []byte, off int64) (n int, err error) {
	return me.readAt(b, off, true)
}

func (me *storagePieceReader) readAt(b []byte, off int64, peerServe bool) (n int, err error) {
	for len(b) != 0 {
		p := me.t.pieceForOffset(off)
		p.waitNoPendingWrites()
		var n1 int
		err = me.getReaderAt(p)
		if err != nil {
			return
		}
		if ps, ok := me.pr.(storage.PeerServePieceReaderAt); ok && peerServe {
			n1, err = ps.ReadAtPeerServe(b, off-p.Info().Offset())
		} else {
			n1, err = me.pr.ReadAt(b, off-p.Info().Offset())
		}
		if n1 == 0 {
			panicif.Nil(err)
			break
		}
		off += int64(n1)
		n += n1
		b = b[n1:]
	}
	return
}

type storageReader interface {
	io.ReaderAt
	io.Closer
}

// PeerServeReaderAt is an optional storage capability: read bytes that are
// about to be sent to a remote peer, as opposed to a local reader.
//
// Mariotte fork (issue #260). A storage backend whose bytes may live remotely
// needs to tell the two apart. A local reader wants the bytes and can afford to
// wait for them; a peer request must not block the serving goroutine on a
// remote fetch, because that serialises every peer read against link latency.
// A backend implementing this may refuse a peer read that it could only satisfy
// by fetching, and start that fetch in the background instead. The refusal
// becomes a normal BitTorrent REJECT.
//
// Backends that do not implement it are read through ReadAt exactly as before.
type PeerServeReaderAt interface {
	ReadAtPeerServe(b []byte, off int64) (int, error)
}

// ExpectedPeerRequestFailure is an optional error capability: a storage
// backend may mark a peer-request read failure as expected (for example a
// cold-piece refusal).
//
// Mariotte fork (issue #295). peerRequestDataReadFailed rate-limits Warning
// logs for these failures per torrent, so a distinct-piece flood cannot emit
// one warning per piece. Errors that do not implement it keep the default
// immediate Warning. The torrent fork does not import Mariotte store code;
// a backend implements the method on its error value.
type ExpectedPeerRequestFailure interface {
	ExpectedPeerRequestFailure()
}

// readAtForPeerServe reads through PeerServeReaderAt when the storage backend
// offers it, and falls back to the plain ReadAt contract otherwise.
func readAtForPeerServe(r storageReader, b []byte, off int64) (int, error) {
	if ps, ok := r.(PeerServeReaderAt); ok {
		return ps.ReadAtPeerServe(b, off)
	}
	return r.ReadAt(b, off)
}

// This wraps a storage impl provided TorrentReader as a storageReader.
type torrentStorageImplReader struct {
	implReader storage.TorrentReader
	t          *Torrent
}

func (me torrentStorageImplReader) ReadAt(p []byte, off int64) (n int, err error) {
	// TODO: Should waitNoPendingWrites take a region?
	me.t.pieceForOffset(off).waitNoPendingWrites()
	return me.implReader.ReadAt(p, off)
}

func (me torrentStorageImplReader) Close() error {
	return me.implReader.Close()
}
