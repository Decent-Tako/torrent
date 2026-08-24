package torrent

import (
	"errors"
	"sync"
	"time"

	"github.com/anacrolix/log"
)

// defaultExpectedPeerRequestLimitInterval bounds expected-failure warnings to
// one visible record per torrent per interval. A production cold-piece storm
// of hundreds of block requests in a few seconds then emits one Warning.
const defaultExpectedPeerRequestLimitInterval = time.Minute

// expectedPeerRequestLimiter rate-limits Warning logs for expected
// peer-request read failures. It lives on Torrent so distinct-piece floods
// cannot each emit a first warning, and two torrents cannot starve each other.
type expectedPeerRequestLimiter struct {
	mu       sync.Mutex
	now      func() time.Time
	interval time.Duration
	lastEmit time.Time
	pending  int64
}

func (l *expectedPeerRequestLimiter) observe() (emit bool, suppressedSinceLast int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.now != nil {
		now = l.now()
	}
	interval := l.interval
	if interval == 0 {
		interval = defaultExpectedPeerRequestLimitInterval
	}
	if l.lastEmit.IsZero() || now.Sub(l.lastEmit) >= interval {
		suppressedSinceLast = l.pending
		l.pending = 0
		l.lastEmit = now
		return true, suppressedSinceLast
	}
	l.pending++
	return false, 0
}

func isExpectedPeerRequestFailure(err error) bool {
	var marker ExpectedPeerRequestFailure
	return errors.As(err, &marker)
}

func (t *Torrent) noteExpectedPeerRequestFailure() (emit bool, suppressedSinceLast int64) {
	emit, suppressedSinceLast = t.expectedPeerRequestFailures.observe()
	t.counters.ExpectedPeerRequestObserved.Add(1)
	if !emit {
		t.counters.ExpectedPeerRequestSuppressed.Add(1)
	}
	return
}

// ExpectedPeerRequestFailureCounts returns the exact observed and suppressed
// counts of expected peer-request read failures for this torrent.
func (t *Torrent) ExpectedPeerRequestFailureCounts() (observed, suppressed int64) {
	return t.counters.ExpectedPeerRequestObserved.Int64(), t.counters.ExpectedPeerRequestSuppressed.Int64()
}

func (c *PeerConn) logPeerRequestDataReadFailed(err error, r Request) {
	if isExpectedPeerRequestFailure(err) {
		emit, suppressed := c.t.noteExpectedPeerRequestFailure()
		if !emit {
			return
		}
		if suppressed > 0 {
			c.logger.Levelf(
				log.Warning,
				"error reading chunk for peer Request %v: %v (suppressed %d similar expected failures)",
				r, err, suppressed,
			)
			return
		}
		c.logger.Levelf(log.Warning, "error reading chunk for peer Request %v: %v", r, err)
		return
	}
	logLevel := log.Warning
	if (c.t.storage != nil && c.t.hasStorageCap()) || c.t.closed.IsSet() {
		// It's expected that pieces might drop. See
		// https://github.com/anacrolix/torrent/issues/702#issuecomment-1000953313. Also the torrent
		// may have been Dropped, and the user expects to own the files, see
		// https://github.com/anacrolix/torrent/issues/980.
		logLevel = log.Debug
	}
	c.logger.Levelf(logLevel, "error reading chunk for peer Request %v: %v", r, err)
}
