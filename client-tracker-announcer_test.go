//go:build go1.25

package torrent

import (
	"log/slog"
	"net/url"
	"testing"
	"testing/synctest"
	"time"

	"github.com/anacrolix/missinggo/v2/panicif"
	"github.com/go-quicktest/qt"
)

// This test doesn't really do much useful anymore. It is useful to break apart the dispatcher a bit
// for testing. It's good to have something that hits up the triggers a bit.
func TestUpdateOverdueRecursion(t *testing.T) {
	// Prevent synctest from tracking some stuff that we don't care about.
	cl := newTestingClient(t)
	synctest.Test(t, func(t *testing.T) {
		d := regularTrackerAnnounceDispatcher{}
		d.initTables()
		d.initTimerNoop()
		d.logger = slog.Default()
		u, _ := url.Parse("http://derp")
		d.initTrackerClient(u, trackerAnnouncerKey(u.String()), cl.config, slog.Default())
		// Two values. One that needs to be marked not overdue on the first call to updateOverdue,
		// and the other that is by a recursive call, and subsequently reversed when we bounce back
		// out to the original call.
		key1 := torrentTrackerAnnouncerKey{}
		key1.ShortInfohash[0] = 1
		key2 := torrentTrackerAnnouncerKey{}
		key2.ShortInfohash[0] = 2
		value1 := nextAnnounceInput{}
		value1.overdue = false
		value1.When = time.Now()
		value2 := nextAnnounceInput{}
		value2.overdue = true
		value2.When = time.Now().Add(4)
		println(value1.When.UnixNano(), value2.When.UnixNano())
		panicif.False(d.announceData.Create(key1, value1))
		panicif.False(d.announceData.Create(key2, value2))
		v2, ok := d.announceData.Get(key2)
		panicif.False(ok)
		expectedValue2 := value2
		expectedValue2.overdue = false
		qt.Check(t, qt.Equals(v2, expectedValue2))
		println(time.Now().UnixNano())
		// This will fix up the values. But if we can advance time and trigger a recursive
		// updateOverdue we can test for thrashing, but it's non-trivial.
		d.updateOverdue()
	})
}

// A dropped torrent can remain in pendingTorrentInputUpdates after a replacement
// with the same infohash has installed fresh announce data. The stale update must
// not replace the new torrent input, or the replacement will never announce Started.
func TestDroppedTorrentInputDoesNotOverwriteReAddedTorrent(t *testing.T) {
	var infoHash shortInfohash
	infoHash[0] = 1
	urlKey := trackerAnnouncerKey("http://tracker.invalid/announce")
	announceKey := torrentTrackerAnnouncerKey{
		ShortInfohash: infoHash,
		url:           urlKey,
	}

	cl := &Client{
		config:              NewDefaultClientConfig(),
		torrents:            make(map[*Torrent]struct{}),
		torrentsByShortHash: make(map[InfoHash]*Torrent),
	}
	old := &Torrent{
		cl:                          cl,
		regularTrackerAnnounceState: make(map[torrentTrackerAnnouncerKey]*announceState),
	}
	readded := &Torrent{
		cl:                          cl,
		regularTrackerAnnounceState: make(map[torrentTrackerAnnouncerKey]*announceState),
	}
	cl.torrents[readded] = struct{}{}
	cl.torrentsByShortHash[infoHash] = readded

	d := &cl.regularTrackerAnnounceDispatcher
	d.torrentClient = cl
	d.logger = slog.Default()
	d.initTables()
	d.initTimerNoop()
	d.trackerClients = map[trackerAnnouncerKey]*trackerClientsValue{
		urlKey: {},
	}
	state := &announceState{}
	d.announceStates = map[torrentTrackerAnnouncerKey]*announceState{
		announceKey: state,
	}
	old.regularTrackerAnnounceState[announceKey] = state
	readded.regularTrackerAnnounceState[announceKey] = state

	panicif.False(d.announceData.Create(announceKey, nextAnnounceInput{
		torrent:                d.makeTorrentInput(readded),
		nextAnnounceStateInput: d.makeAnnounceStateInput(announceKey),
	}))
	want, ok := d.announceData.Get(announceKey)
	qt.Assert(t, qt.IsTrue(ok))
	qt.Assert(t, qt.IsTrue(want.torrent.Ok))

	d.updateTorrentInput(old)

	got, ok := d.announceData.Get(announceKey)
	qt.Assert(t, qt.IsTrue(ok))
	qt.Assert(t, qt.Equals(got, want))
}
