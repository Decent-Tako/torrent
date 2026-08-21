package torrent

import (
	"encoding/binary"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	qt "github.com/go-quicktest/qt"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/internal/testutil"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/tracker"
)

func TestClientInvalidTracker(t *testing.T) {
	timeout := time.NewTimer(3 * time.Second)
	receivedStatusUpdate := make(chan bool)
	gotTrackerDisconnectedEvt := false
	cfg := TestingConfig(t)
	cfg.DisableTrackers = false
	cfg.Callbacks.StatusUpdated = append(cfg.Callbacks.StatusUpdated, func(e StatusUpdatedEvent) {
		if e.Event == TrackerAnnounceError {
			// ignore
			return
		}
		if e.Event == TrackerDisconnected {
			gotTrackerDisconnectedEvt = true
			qt.Assert(t, qt.Equals(e.Url, "ws://test.invalid:4242"))
			qt.Assert(t, qt.IsNotNil(e.Error))
		}
		receivedStatusUpdate <- true
	})

	cl, err := NewClient(cfg)
	qt.Assert(t, qt.IsNil(err))
	defer cl.Close()

	dir, mi := testutil.GreetingTestTorrent()
	defer os.RemoveAll(dir)

	mi.AnnounceList = [][]string{
		{"ws://test.invalid:4242"},
	}

	to, err := cl.AddTorrent(mi)
	qt.Assert(t, qt.IsNil(err))

	select {
	case <-timeout.C:
	case <-receivedStatusUpdate:
	}
	qt.Assert(t, qt.IsTrue(gotTrackerDisconnectedEvt))
	to.Drop()
}

var upgrader = websocket.Upgrader{}

func testtracker(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer c.Close()
	for {
		_, _, err := c.ReadMessage()
		if err != nil {
			break
		}
		//err = c.WriteMessage(mt, message)
		//if err != nil {
		//	break
		//}
	}
}

func TestClientValidTrackerConn(t *testing.T) {
	s, trackerUrl := startTestTracker()
	defer s.Close()

	timeout := time.NewTimer(3 * time.Second)
	receivedStatusUpdate := make(chan bool)
	gotTrackerConnectedEvt := false
	cfg := TestingConfig(t)
	cfg.DisableTrackers = false
	cfg.Callbacks.StatusUpdated = append(cfg.Callbacks.StatusUpdated, func(e StatusUpdatedEvent) {
		if e.Event == TrackerConnected {
			gotTrackerConnectedEvt = true
			qt.Assert(t, qt.Equals(e.Url, trackerUrl))
			qt.Assert(t, qt.IsNil(e.Error))
		}
		receivedStatusUpdate <- true
	})

	cl, err := NewClient(cfg)
	qt.Assert(t, qt.IsNil(err))
	defer cl.Close()

	dir, mi := testutil.GreetingTestTorrent()
	defer os.RemoveAll(dir)

	mi.AnnounceList = [][]string{
		{trackerUrl},
	}

	to, err := cl.AddTorrent(mi)
	qt.Assert(t, qt.IsNil(err))

	select {
	case <-timeout.C:
	case <-receivedStatusUpdate:
	}
	qt.Assert(t, qt.IsTrue(gotTrackerConnectedEvt))
	to.Drop()
}

func TestClientAnnounceFailure(t *testing.T) {
	s, trackerUrl := startTestTracker()
	defer s.Close()

	timeout := time.NewTimer(3 * time.Second)
	receivedStatusUpdate := make(chan bool)
	gotTrackerAnnounceErrorEvt := false
	cfg := TestingConfig(t)
	cfg.DisableTrackers = false

	var to *Torrent

	cfg.Callbacks.StatusUpdated = append(cfg.Callbacks.StatusUpdated, func(e StatusUpdatedEvent) {
		if e.Event == TrackerConnected {
			// ignore
			return
		}
		if e.Event == TrackerAnnounceError {
			gotTrackerAnnounceErrorEvt = true
			qt.Assert(t, qt.Equals(e.Url, trackerUrl))
			qt.Assert(t, qt.Equals(e.InfoHash, to.InfoHash().HexString()))
			qt.Assert(t, qt.IsNotNil(e.Error))
			qt.Assert(t, qt.Equals(e.Error.Error(), "test error"))
		}
		receivedStatusUpdate <- true
	})

	cl, err := NewClient(cfg)
	qt.Assert(t, qt.IsNil(err))
	defer cl.Close()

	cl.websocketTrackers.GetAnnounceRequest = func(event tracker.AnnounceEvent, infoHash [20]byte) (tracker.AnnounceRequest, error) {
		return tracker.AnnounceRequest{}, errors.New("test error")
	}

	dir, mi := testutil.GreetingTestTorrent()
	defer os.RemoveAll(dir)

	mi.AnnounceList = [][]string{
		{trackerUrl},
	}

	to, err = cl.AddTorrent(mi)
	qt.Assert(t, qt.IsNil(err))

	select {
	case <-timeout.C:
	case <-receivedStatusUpdate:
	}
	qt.Assert(t, qt.IsTrue(gotTrackerAnnounceErrorEvt))
	to.Drop()
}

func TestClientAnnounceSuccess(t *testing.T) {
	s, trackerUrl := startTestTracker()
	defer s.Close()

	timeout := time.NewTimer(3 * time.Second)
	receivedStatusUpdate := make(chan bool)
	gotTrackerAnnounceSuccessfulEvt := false
	cfg := TestingConfig(t)
	cfg.DisableTrackers = false

	var to *Torrent

	cfg.Callbacks.StatusUpdated = append(cfg.Callbacks.StatusUpdated, func(e StatusUpdatedEvent) {
		if e.Event == TrackerConnected {
			// ignore
			return
		}
		if e.Event == TrackerAnnounceSuccessful {
			gotTrackerAnnounceSuccessfulEvt = true
			qt.Assert(t, qt.Equals(e.Url, trackerUrl))
			qt.Assert(t, qt.Equals(e.InfoHash, to.InfoHash().HexString()))
			qt.Assert(t, qt.IsNil(e.Error))
		}
		receivedStatusUpdate <- true
	})

	cl, err := NewClient(cfg)
	qt.Assert(t, qt.IsNil(err))
	defer cl.Close()

	dir, mi := testutil.GreetingTestTorrent()
	defer os.RemoveAll(dir)

	mi.AnnounceList = [][]string{
		{trackerUrl},
	}

	to, err = cl.AddTorrent(mi)
	qt.Assert(t, qt.IsNil(err))

	select {
	case <-timeout.C:
	case <-receivedStatusUpdate:
	}
	qt.Assert(t, qt.IsTrue(gotTrackerAnnounceSuccessfulEvt))
	to.Drop()
}

func startTestTracker() (*httptest.Server, string) {
	s := httptest.NewServer(http.HandlerFunc(testtracker))
	trackerUrl := "ws" + strings.TrimPrefix(s.URL, "http")
	return s, trackerUrl
}

func TestTrackerDropAndReAddSameInfoHash(t *testing.T) {
	seederDir, mi := testutil.GreetingTestTorrent()
	defer os.RemoveAll(seederDir)

	seederConfig := TestingConfig(t)
	seederConfig.DataDir = seederDir
	seederConfig.Seed = true
	seeder, err := NewClient(seederConfig)
	require.NoError(t, err)
	defer seeder.Close()
	seeded, err := seeder.AddTorrent(mi)
	require.NoError(t, err)
	require.NoError(t, seeded.VerifyData())
	require.Eventually(t, seeded.Seeding, 5*time.Second, 10*time.Millisecond)

	peer := compactPeerForTest(t, net.IPv4(127, 0, 0, 1), seeder.LocalPort())
	var announces atomic.Int64
	var startedAnnounces atomic.Int64
	var stoppedAnnounces atomic.Int64
	firstStoppedAnnounce := make(chan struct{})
	releaseFirstStoppedAnnounce := make(chan struct{})
	var releaseFirstStoppedAnnounceOnce sync.Once
	releaseFirstStopped := func() {
		releaseFirstStoppedAnnounceOnce.Do(func() {
			close(releaseFirstStoppedAnnounce)
		})
	}
	t.Cleanup(releaseFirstStopped)
	testTracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		announces.Add(1)
		switch r.URL.Query().Get("event") {
		case "started":
			startedAnnounces.Add(1)
		case "stopped":
			if stoppedAnnounces.Add(1) == 1 {
				close(firstStoppedAnnounce)
				<-releaseFirstStoppedAnnounce
			}
		}
		w.Header().Set("Content-Type", "text/plain")
		if err := bencode.NewEncoder(w).Encode(map[string]any{
			"interval": 3600,
			"peers":    peer,
		}); err != nil {
			t.Errorf("write tracker response: %v", err)
		}
	}))
	defer testTracker.Close()

	leecherConfig := TestingConfig(t)
	leecherConfig.DisableTrackers = false
	leecher, err := NewClient(leecherConfig)
	require.NoError(t, err)
	defer leecher.Close()

	magnet := metainfo.Magnet{
		InfoHash: mi.HashInfoBytes(),
		Trackers: []string{testTracker.URL},
	}.String()
	first, err := leecher.AddMagnet(magnet)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return first.Info() != nil && first.Stats().ActivePeers == 1 && announces.Load() == 1
	}, 5*time.Second, 10*time.Millisecond)

	beforePeers := first.Stats().ActivePeers
	announcesBeforeDrop := announces.Load()
	first.Drop()
	<-first.Closed()
	<-firstStoppedAnnounce
	first = nil
	runtime.GC()
	announcesAfterDrop := announces.Load()

	readded, err := leecher.AddMagnet(magnet)
	require.NoError(t, err)
	releaseFirstStopped()
	require.Eventually(t, func() bool {
		return readded.Info() != nil && readded.Stats().ActivePeers == 1 &&
			announces.Load() == 3 && startedAnnounces.Load() == 2
	}, 5*time.Second, 10*time.Millisecond)
	afterPeers := readded.Stats().ActivePeers
	announcesAfterReAdd := announces.Load()
	metadataResolved := readded.Info() != nil
	t.Logf("before_peers=%d tracker_announces_before_drop=%d tracker_announces_after_drop=%d tracker_stopped_announces_after_drop=%d after_readd_peers=%d tracker_announces_after_readd=%d tracker_started_announces_after_readd=%d metadata_resolved=%t",
		beforePeers, announcesBeforeDrop, announcesAfterDrop, stoppedAnnounces.Load(), afterPeers,
		announcesAfterReAdd, startedAnnounces.Load(), metadataResolved)

	var key torrentTrackerAnnouncerKey
	for key = range readded.regularTrackerAnnounceState {
		break
	}
	require.NotZero(t, key)
	readded.Drop()
	require.Eventually(t, func() bool {
		leecher.lock()
		defer leecher.unlock()
		_, hasAnnounceState := leecher.regularTrackerAnnounceDispatcher.announceStates[key]
		return announces.Load() == 4 && stoppedAnnounces.Load() == 2 &&
			!leecher.regularTrackerAnnounceDispatcher.announceData.ContainsKey(key) && !hasAnnounceState
	}, 2*time.Second, 10*time.Millisecond)
}

func compactPeerForTest(t *testing.T, ip net.IP, port int) []byte {
	t.Helper()
	ip = ip.To4()
	require.NotNil(t, ip)
	require.Greater(t, port, 0)
	require.LessOrEqual(t, port, 65535)
	peer := make([]byte, 6)
	copy(peer, ip)
	binary.BigEndian.PutUint16(peer[4:], uint16(port))
	return peer
}
