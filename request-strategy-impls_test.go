package torrent

import (
	"context"
	"io"
	"runtime"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring/v2"
	g "github.com/anacrolix/generics"
	"github.com/anacrolix/missinggo/v2/iter"
	"github.com/go-quicktest/qt"

	requestStrategy "github.com/anacrolix/torrent/internal/request-strategy"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	infohash_v2 "github.com/anacrolix/torrent/types/infohash-v2"
)

// Assigned to keep the result alive so the call below isn't optimised away, without itself
// allocating.
var requestStrategyResultSink bool

func TestRequestStrategyPieceDoesntAlloc(t *testing.T) {
	akshalTorrent := &Torrent{pieces: make([]pieceState, 1)}
	// Query through the interface, as the request strategy does. Piece is now an index-based call
	// returning a bool, so no per-piece value is boxed and nothing is allocated.
	var input requestStrategy.Torrent = requestStrategyTorrent{akshalTorrent}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	requestStrategyResultSink = input.PieceRequest(0)
	runtime.ReadMemStats(&after)
	qt.Assert(t, qt.Equals(before.HeapAlloc, after.HeapAlloc))
}

type storagePiece struct {
	complete bool
}

func (s storagePiece) ReadAt(p []byte, off int64) (n int, err error) {
	//TODO implement me
	panic("implement me")
}

func (s storagePiece) WriteAt(p []byte, off int64) (n int, err error) {
	//TODO implement me
	panic("implement me")
}

func (s storagePiece) MarkComplete() error {
	//TODO implement me
	panic("implement me")
}

func (s storagePiece) MarkNotComplete() error {
	//TODO implement me
	panic("implement me")
}

func (s storagePiece) Completion() storage.Completion {
	return storage.Completion{Ok: true, Complete: s.complete}
}

var _ storage.PieceImpl = storagePiece{}

type mutableCompletionPiece struct {
	complete *bool
}

func (p mutableCompletionPiece) ReadAt([]byte, int64) (int, error) {
	return 0, io.EOF
}

func (p mutableCompletionPiece) WriteAt(b []byte, _ int64) (int, error) {
	return len(b), nil
}

func (p mutableCompletionPiece) MarkComplete() error {
	*p.complete = true
	return nil
}

func (p mutableCompletionPiece) MarkNotComplete() error {
	*p.complete = false
	return nil
}

func (p mutableCompletionPiece) Completion() storage.Completion {
	return storage.Completion{Ok: true, Complete: *p.complete}
}

type sharedCompletionStorage struct {
	complete *bool
	capacity storage.TorrentCapacity
}

type countingWakePeer struct {
	want        int
	lowChecks   int
	wakes       int
	done        chan struct{}
	pieces      roaring.Bitmap
	scanStarted chan struct{}
	releaseScan <-chan struct{}
}

func (p *countingWakePeer) isLowOnRequests() bool {
	if p.scanStarted != nil {
		select {
		case p.scanStarted <- struct{}{}:
		default:
		}
		<-p.releaseScan
	}
	p.lowChecks++
	return true
}

func (p *countingWakePeer) onNeedUpdateRequests(updateRequestReason) {
	p.wakes++
	if p.wakes == p.want {
		close(p.done)
	}
}

func (*countingWakePeer) handleCancel(RequestIndex)      {}
func (*countingWakePeer) cancelAllRequests()             {}
func (*countingWakePeer) connectionFlags() string        { return "" }
func (*countingWakePeer) onClose()                       {}
func (*countingWakePeer) onGotInfo(*metainfo.Info)       {}
func (*countingWakePeer) drop()                          {}
func (*countingWakePeer) providedBadData()               {}
func (*countingWakePeer) String() string                 { return "counting-wake-peer" }
func (*countingWakePeer) peerImplStatusLines() []string  { return nil }
func (*countingWakePeer) peerImplWriteStatus(io.Writer)  {}
func (*countingWakePeer) peerHasAllPieces() (bool, bool) { return false, false }
func (p *countingWakePeer) peerPieces() *roaring.Bitmap  { return &p.pieces }

func (s sharedCompletionStorage) OpenTorrent(
	context.Context,
	*metainfo.Info,
	metainfo.Hash,
) (storage.TorrentImpl, error) {
	return storage.TorrentImpl{
		Piece: func(metainfo.Piece) storage.PieceImpl {
			return mutableCompletionPiece{complete: s.complete}
		},
		Capacity: s.capacity,
	}, nil
}

func TestPieceCompletionWakesIdlePeerSharingRequestOrder(t *testing.T) {
	cl := newTestingClient(t)
	complete := false
	capacityFn := func() (int64, bool) { return 1 << 20, true }
	storageClient := sharedCompletionStorage{
		complete: &complete,
		capacity: &capacityFn,
	}
	first, _ := cl.AddTorrentOpt(AddTorrentOpts{
		InfoHash:                 metainfo.Hash{1},
		Storage:                  storageClient,
		DisableInitialPieceCheck: true,
	})
	second, _ := cl.AddTorrentOpt(AddTorrentOpts{
		InfoHash:                 metainfo.Hash{2},
		Storage:                  storageClient,
		DisableInitialPieceCheck: true,
	})
	info := &metainfo.Info{
		Name:        "shared-request-order",
		Length:      1,
		PieceLength: 1,
		Pieces:      make([]byte, metainfo.HashSize),
	}
	qt.Assert(t, qt.IsNil(first.setInfoUnlocked(info)))
	qt.Assert(t, qt.IsNil(second.setInfoUnlocked(info)))
	first.DownloadAll()
	second.DownloadAll()

	peer := PeerConn{Peer: Peer{cl: cl, t: second}}
	peer.initRequestState()
	peer.legacyPeerImpl = &peer
	second.conns[&peer] = struct{}{}

	cl.lock()
	complete = true
	qt.Assert(t, qt.IsTrue(first.updatePieceCompletion(0)))
	cl.unlock()

	wantReason := updateRequestReason("Torrent.updatePieceCompletion")
	deadline := time.Now().Add(time.Second)
	for {
		cl.rLock()
		gotReason := peer.needRequestUpdate
		cl.rUnlock()
		if gotReason == wantReason {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("peer request-update reason = %q, want %q", gotReason, wantReason)
		}
		time.Sleep(time.Millisecond)
	}
	cl.lock()
	delete(second.conns, &peer)
	cl.unlock()
}

func TestRequestOrderWakeDoesNotHoldStatsLockAfterRestoredCorpusReady(t *testing.T) {
	cl := newTestingClient(t)
	complete := false
	capacityFn := func() (int64, bool) { return 1 << 20, true }
	storageClient := sharedCompletionStorage{
		complete: &complete,
		capacity: &capacityFn,
	}
	first, _ := cl.AddTorrentOpt(AddTorrentOpts{
		InfoHash:                 metainfo.Hash{1},
		Storage:                  storageClient,
		DisableInitialPieceCheck: true,
	})
	info := &metainfo.Info{
		Name:        "restored-corpus-stats-lock",
		Length:      32,
		PieceLength: 1,
		Pieces:      make([]byte, 32*metainfo.HashSize),
	}
	qt.Assert(t, qt.IsNil(first.setInfoUnlocked(info)))

	const torrentCount = 128
	var peerTorrent *Torrent
	for i := 1; i < torrentCount; i++ {
		candidate, _ := cl.AddTorrentOpt(AddTorrentOpts{
			InfoHash:                 metainfo.Hash{2, byte(i)},
			Storage:                  storageClient,
			DisableInitialPieceCheck: true,
		})
		qt.Assert(t, qt.IsNil(candidate.setInfoUnlocked(info)))
		peerTorrent = candidate
	}

	scanStarted := make(chan struct{}, 1)
	releaseScan := make(chan struct{})
	counter := &countingWakePeer{
		want:        1,
		done:        make(chan struct{}),
		scanStarted: scanStarted,
		releaseScan: releaseScan,
	}
	peer := &PeerConn{Peer: Peer{cl: cl, t: peerTorrent}}
	peer.legacyPeerImpl = counter
	peerTorrent.conns[peer] = struct{}{}
	defer func() {
		cl.lock()
		delete(peerTorrent.conns, peer)
		cl.unlock()
	}()

	cl.lock()
	complete = true
	for piece := range 32 {
		first.setInitialPieceCompletionFromStorage(piece)
	}
	cl.unlock()

	select {
	case <-scanStarted:
		statsDone := make(chan struct{})
		go func() {
			first.Stats()
			close(statsDone)
		}()
		select {
		case <-statsDone:
			close(releaseScan)
		case <-time.After(100 * time.Millisecond):
			close(releaseScan)
			<-statsDone
			t.Fatal("Stats waited behind a restored-corpus request-order wake holding the client lock")
		}
	case <-time.After(100 * time.Millisecond):
		close(releaseScan)
	}

	cl.lock()
	complete = false
	for piece := range 8 {
		first.updatePieceCompletion(piece)
	}
	complete = true
	for piece := range 8 {
		first.updatePieceCompletion(piece)
	}
	cl.unlock()

	for measurement := range 3 {
		statsDone := make(chan struct{})
		go func() {
			first.Stats()
			close(statsDone)
		}()
		select {
		case <-statsDone:
		case <-time.After(250 * time.Millisecond):
			t.Fatalf("Stats measurement %d waited behind request-order wake", measurement+1)
		}
	}
	select {
	case <-counter.done:
	case <-time.After(time.Second):
		t.Fatal("live completion did not wake the shared-order peer")
	}

	cl.lock()
	lowChecks := counter.lowChecks
	wakes := counter.wakes
	cl.unlock()
	qt.Check(t, qt.Equals(lowChecks, 1))
	qt.Check(t, qt.Equals(wakes, 1))
}

func TestRequestOrderWakeCoalescesRestoredCorpusBurst(t *testing.T) {
	cl := newTestingClient(t)
	complete := false
	capacityFn := func() (int64, bool) { return 1 << 20, true }
	storageClient := sharedCompletionStorage{
		complete: &complete,
		capacity: &capacityFn,
	}
	first, _ := cl.AddTorrentOpt(AddTorrentOpts{
		InfoHash:                 metainfo.Hash{1},
		Storage:                  storageClient,
		DisableInitialPieceCheck: true,
	})
	info := &metainfo.Info{
		Name:        "restored-corpus-wake",
		Length:      32,
		PieceLength: 1,
		Pieces:      make([]byte, 32*metainfo.HashSize),
	}
	qt.Assert(t, qt.IsNil(first.setInfoUnlocked(info)))

	const peerCount = 132
	counter := &countingWakePeer{
		want: peerCount,
		done: make(chan struct{}),
	}
	peers := make([]*PeerConn, 0, peerCount)
	peerTorrents := make([]*Torrent, 0, peerCount)
	for i := range peerCount {
		candidate, _ := cl.AddTorrentOpt(AddTorrentOpts{
			InfoHash:                 metainfo.Hash{2, byte(i)},
			Storage:                  storageClient,
			DisableInitialPieceCheck: true,
		})
		qt.Assert(t, qt.IsNil(candidate.setInfoUnlocked(info)))
		peer := &PeerConn{Peer: Peer{cl: cl, t: candidate}}
		peer.legacyPeerImpl = counter
		candidate.conns[peer] = struct{}{}
		peers = append(peers, peer)
		peerTorrents = append(peerTorrents, candidate)
	}

	cl.lock()
	complete = true
	for piece := range 32 {
		first.updatePieceCompletion(piece)
	}
	cl.unlock()

	select {
	case <-counter.done:
	case <-time.After(time.Second):
		t.Fatal("shared request-order peers were not woken")
	}

	cl.lock()
	for i, peer := range peers {
		delete(peerTorrents[i].conns, peer)
	}
	lowChecks := counter.lowChecks
	cl.unlock()
	qt.Check(t, qt.Equals(lowChecks, peerCount))
}

type storageClient struct {
	completed int
}

func (s *storageClient) OpenTorrent(
	_ context.Context,
	info *metainfo.Info,
	infoHash metainfo.Hash,
) (storage.TorrentImpl, error) {
	return storage.TorrentImpl{
		Piece: func(p metainfo.Piece) storage.PieceImpl {
			return storagePiece{complete: p.Index() < s.completed}
		},
	}, nil
}

func BenchmarkRequestStrategy(b *testing.B) {
	cl := newTestingClient(b)
	storageClient := storageClient{}
	tor, new := cl.AddTorrentOpt(AddTorrentOpts{
		InfoHash:   testingTorrentInfoHash,
		InfoHashV2: g.Some(infohash_v2.FromHexString("deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")),
		Storage:    &storageClient,
	})
	tor.disableTriggers = true
	qt.Assert(b, qt.IsTrue(new))
	const pieceLength = 1 << 8 << 10
	const numPieces = 10_000
	err := tor.setInfoUnlocked(&metainfo.Info{
		Pieces:      make([]byte, numPieces*metainfo.HashSize),
		PieceLength: pieceLength,
		Length:      pieceLength * numPieces,
	})
	qt.Assert(b, qt.IsNil(err))
	peer := cl.newConnection(nil, newConnectionOpts{
		network: "test",
	})
	peer.setTorrent(tor)
	qt.Assert(b, qt.IsNotNil(tor.storage))
	const chunkSize = defaultChunkSize
	peer.onPeerHasAllPiecesNoTriggers()
	tor.cl.lock()
	for i := 0; i < tor.numPieces(); i++ {
		tor.pieces[i].priority.Raise(PiecePriorityNormal)
		tor.updatePiecePriorityNoRequests(i)
	}
	tor.cl.unlock()
	peer.peerChoking = false
	for b.Loop() {
		storageClient.completed = 0
		for pieceIndex := range iter.N(numPieces) {
			tor.cl.lock()
			tor.updatePieceCompletion(pieceIndex)
			tor.cl.unlock()
		}
		for completed := 0; completed <= numPieces; completed += 1 {
			storageClient.completed = completed
			if completed > 0 {
				func() {
					tor.cl.lock()
					defer tor.cl.unlock()
					tor.updatePieceCompletion(completed - 1)
				}()
			}
			// Starting and stopping timers around this part causes lots of GC overhead.
			rs := peer.getDesiredRequestState()
			tor.cacheNextRequestIndexesForReuse(rs.Requests.requestIndexes)
			// End of part that should be timed.
			remainingChunks := (numPieces - completed) * (pieceLength / chunkSize)
			qt.Assert(b, qt.HasLen(rs.Requests.requestIndexes, min(
				remainingChunks,
				int(cl.config.MaxUnverifiedBytes/chunkSize))))
		}
	}
}
