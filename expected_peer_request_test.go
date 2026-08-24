package torrent

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/log"
	"github.com/go-quicktest/qt"

	pp "github.com/anacrolix/torrent/peer_protocol"
)

// expectedPeerRequestError is a test stand-in for Mariotte's cold-piece
// refusal. The torrent fork classifies by this optional method, not by importing
// the store.
type expectedPeerRequestError struct {
	s string
}

func (e expectedPeerRequestError) Error() string { return e.s }

func (e expectedPeerRequestError) ExpectedPeerRequestFailure() {}

type recordingLogHandler struct {
	mu      sync.Mutex
	records []log.Record
}

func (h *recordingLogHandler) Handle(r log.Record) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
}

func (h *recordingLogHandler) snapshot() []log.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]log.Record, len(h.records))
	copy(out, h.records)
	return out
}

func chunkReadWarnings(records []log.Record) []log.Record {
	var out []log.Record
	for _, r := range records {
		if r.Level != log.Warning {
			continue
		}
		if !strings.Contains(r.Text(), "error reading chunk for peer Request") {
			continue
		}
		out = append(out, r)
	}
	return out
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newPeerConnForRequestFailureTest(t *testing.T, cl *Client, tor *Torrent) (*PeerConn, *recordingLogHandler) {
	t.Helper()
	pc := cl.newConnection(nil, newConnectionOpts{network: "test"})
	pc.setTorrent(tor)
	pc.PeerExtensionBytes.SetBit(pp.ExtensionBitFast, true)
	pc.initMessageWriter()
	h := &recordingLogHandler{}
	pc.logger.SetHandlers(h)
	return pc, h
}

func firePeerRequestReadFailed(t *testing.T, cl *Client, pc *PeerConn, err error, piece int) {
	t.Helper()
	req := Request{Index: pp.Integer(piece), ChunkSpec: ChunkSpec{Begin: 0, Length: 1}}
	cl.lock()
	defer cl.unlock()
	pc.peerRequestDataReadFailed(err, req)
}

// A storm of at least 1,000 expected cold refusals across multiple pieces must
// emit one Warning, keep exact counters, and report suppression on the next
// visible record.
func TestExpectedPeerRequestFailureStormIsBounded(t *testing.T) {
	cl := newTestingClient(t)
	tor := cl.newTorrentForTesting()
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	tor.expectedPeerRequestFailures.now = clock.Now
	tor.expectedPeerRequestFailures.interval = time.Minute
	pc, h := newPeerConnForRequestFailureTest(t, cl, tor)

	const n = 1000
	const pieces = 10
	err := expectedPeerRequestError{s: "cold piece not ready"}
	for i := 0; i < n; i++ {
		firePeerRequestReadFailed(t, cl, pc, err, i%pieces)
	}

	warnings := chunkReadWarnings(h.snapshot())
	qt.Assert(t, qt.Equals(len(warnings), 1), qt.Commentf("expected one Warning for a 1,000-request storm, got %d", len(warnings)))
	qt.Assert(t, qt.Equals(strings.Contains(warnings[0].Text(), "suppressed"), false), qt.Commentf("first visible record must not report suppression: %q", warnings[0].Text()))

	observed, suppressed := tor.ExpectedPeerRequestFailureCounts()
	qt.Assert(t, qt.Equals(observed, int64(n)))
	qt.Assert(t, qt.Equals(suppressed, int64(n-1)))

	clock.Advance(time.Minute)
	firePeerRequestReadFailed(t, cl, pc, err, 0)
	warnings = chunkReadWarnings(h.snapshot())
	qt.Assert(t, qt.Equals(len(warnings), 2), qt.Commentf("next visible record after the interval: got %d warnings", len(warnings)))
	qt.Assert(t, qt.IsTrue(strings.Contains(warnings[1].Text(), fmt.Sprintf("suppressed %d similar expected failures", n-1))), qt.Commentf("next visible record must report suppression: %q", warnings[1].Text()))

	observed, suppressed = tor.ExpectedPeerRequestFailureCounts()
	qt.Assert(t, qt.Equals(observed, int64(n+1)))
	qt.Assert(t, qt.Equals(suppressed, int64(n-1)))
}

// Two torrents each emit their first expected-failure Warning. A client-global
// limiter would suppress the second torrent.
func TestExpectedPeerRequestFailureLimiterIsPerTorrent(t *testing.T) {
	cl := newTestingClient(t)
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	a := cl.newTorrentForTesting()
	b := cl.newTorrentOpt(AddTorrentOpts{
		InfoHash:                 [20]byte{2},
		DisableInitialPieceCheck: true,
	})
	for _, tor := range []*Torrent{a, b} {
		tor.expectedPeerRequestFailures.now = clock.Now
		tor.expectedPeerRequestFailures.interval = time.Minute
	}
	pcA, hA := newPeerConnForRequestFailureTest(t, cl, a)
	pcB, hB := newPeerConnForRequestFailureTest(t, cl, b)
	err := expectedPeerRequestError{s: "cold piece not ready"}

	firePeerRequestReadFailed(t, cl, pcA, err, 0)
	firePeerRequestReadFailed(t, cl, pcB, err, 1)

	qt.Assert(t, qt.Equals(len(chunkReadWarnings(hA.snapshot())), 1))
	qt.Assert(t, qt.Equals(len(chunkReadWarnings(hB.snapshot())), 1))
	obsA, supA := a.ExpectedPeerRequestFailureCounts()
	obsB, supB := b.ExpectedPeerRequestFailureCounts()
	qt.Assert(t, qt.Equals(obsA, int64(1)))
	qt.Assert(t, qt.Equals(supA, int64(0)))
	qt.Assert(t, qt.Equals(obsB, int64(1)))
	qt.Assert(t, qt.Equals(supB, int64(0)))
}

// An unexpected storage error still emits Warning immediately, even inside an
// expected-failure rate-limit window.
func TestUnexpectedPeerRequestFailureEmitsWarningImmediately(t *testing.T) {
	cl := newTestingClient(t)
	tor := cl.newTorrentForTesting()
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	tor.expectedPeerRequestFailures.now = clock.Now
	tor.expectedPeerRequestFailures.interval = time.Minute
	pc, h := newPeerConnForRequestFailureTest(t, cl, tor)

	firePeerRequestReadFailed(t, cl, pc, expectedPeerRequestError{s: "cold piece not ready"}, 0)
	firePeerRequestReadFailed(t, cl, pc, errors.New("disk full"), 1)

	warnings := chunkReadWarnings(h.snapshot())
	qt.Assert(t, qt.Equals(len(warnings), 2), qt.Commentf("unexpected error must not share the expected-failure limiter: %v", warnings))
	qt.Assert(t, qt.IsTrue(strings.Contains(warnings[1].Text(), "disk full")), qt.Commentf("%q", warnings[1].Text()))
	observed, suppressed := tor.ExpectedPeerRequestFailureCounts()
	qt.Assert(t, qt.Equals(observed, int64(1)))
	qt.Assert(t, qt.Equals(suppressed, int64(0)))
}

// Errors that do not implement the optional marker keep the default immediate
// Warning path. Two unmarked failures are two warnings.
func TestUnmarkedPeerRequestFailureKeepsImmediateWarning(t *testing.T) {
	cl := newTestingClient(t)
	tor := cl.newTorrentForTesting()
	pc, h := newPeerConnForRequestFailureTest(t, cl, tor)
	firePeerRequestReadFailed(t, cl, pc, errors.New("io error"), 0)
	firePeerRequestReadFailed(t, cl, pc, errors.New("io error"), 1)
	qt.Assert(t, qt.Equals(len(chunkReadWarnings(h.snapshot())), 2))
	observed, suppressed := tor.ExpectedPeerRequestFailureCounts()
	qt.Assert(t, qt.Equals(observed, int64(0)))
	qt.Assert(t, qt.Equals(suppressed, int64(0)))
}

func TestExpectedPeerRequestLimiterRace(t *testing.T) {
	var l expectedPeerRequestLimiter
	l.interval = time.Hour
	const goroutines = 8
	const each = 1000
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < each; j++ {
				l.observe()
			}
		}()
	}
	wg.Wait()
	qt.Assert(t, qt.Equals(l.pending, int64(goroutines*each-1)))
}
