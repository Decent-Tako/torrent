package storage

import (
	"errors"
	"io"
	"testing"

	"github.com/anacrolix/torrent/metainfo"
)

// Mariotte fork (#260). The peer-serve marker is only useful if it survives the
// default PieceReader wrapper. Piece.NewReader falls back to a nopCloser struct
// for any PieceImpl that does not implement PieceReaderer, which includes
// Mariotte's own store; if that fallback dropped the capability, every cold peer
// read would silently go back to blocking on object storage.

var errRefusedPeerServe = errors.New("refused peer serve")

// peerServePiece is a PieceImpl that distinguishes the two read paths.
type peerServePiece struct {
	data []byte
}

func (p *peerServePiece) ReadAt(b []byte, off int64) (int, error) {
	if off >= int64(len(p.data)) {
		return 0, io.EOF
	}
	return copy(b, p.data[off:]), nil
}

func (p *peerServePiece) ReadAtPeerServe(b []byte, off int64) (int, error) {
	return 0, errRefusedPeerServe
}

func (p *peerServePiece) WriteAt(b []byte, off int64) (int, error) { return 0, io.ErrClosedPipe }
func (p *peerServePiece) MarkComplete() error                      { return nil }
func (p *peerServePiece) MarkNotComplete() error                   { return nil }
func (p *peerServePiece) Completion() Completion {
	return Completion{Complete: true, Ok: true}
}

func newPeerServeTestPiece(data []byte) Piece {
	info := &metainfo.Info{
		PieceLength: int64(len(data)),
		Pieces:      make([]byte, 20),
		Length:      int64(len(data)),
	}
	return Piece{PieceImpl: &peerServePiece{data: data}, mip: info.Piece(0)}
}

// A plain read goes through ReadAt and returns the bytes.
func TestPieceReadAtIsNotPeerServe(t *testing.T) {
	p := newPeerServeTestPiece([]byte("hello world"))
	b := make([]byte, 5)
	n, err := p.ReadAt(b, 0)
	if err != nil || n != 5 || string(b) != "hello" {
		t.Fatalf("local ReadAt: n=%d err=%v b=%q", n, err, b)
	}
}

// The default wrapper must carry PeerServePieceReaderAt through, so a caller
// that asks for a peer-serve read still reaches the backend's refusal.
func TestNewReaderPreservesPeerServeCapability(t *testing.T) {
	p := newPeerServeTestPiece([]byte("hello world"))
	r, err := p.NewReader()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	ps, ok := r.(PeerServePieceReaderAt)
	if !ok {
		t.Fatal("default PieceReader wrapper dropped PeerServePieceReaderAt: every cold peer read would block again")
	}
	if _, err := ps.ReadAtPeerServe(make([]byte, 5), 0); !errors.Is(err, errRefusedPeerServe) {
		t.Fatalf("peer-serve read did not reach the backend: %v", err)
	}
	// The same reader must still serve a normal local read.
	b := make([]byte, 5)
	if n, err := r.ReadAt(b, 0); err != nil || n != 5 || string(b) != "hello" {
		t.Fatalf("local read through wrapper: n=%d err=%v b=%q", n, err, b)
	}
}
