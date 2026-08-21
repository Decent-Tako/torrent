package httpTracker

import (
	"net/url"
	"testing"

	"github.com/anacrolix/torrent/tracker/shared"
	"github.com/anacrolix/torrent/tracker/udp"
)

// BEP 21, "Tracker Announce": "In order to tell the tracker that a peer is a
// partial seed, it MUST send an event=paused parameter in every announce while
// it is a partial seed."
//
// This is the normative tracker half of BEP 21. Decent-Tako/Mariotte#161 added
// only upload_only, which BEP 21 defines for the extension handshake, not the
// tracker. Without event=paused a BEP 21 tracker cannot tell us apart from an
// ordinary downloader, which is the whole point of the extension.
func TestPartialSeedAnnouncesEventPaused(t *testing.T) {
	u := &url.URL{Path: "/announce"}
	setAnnounceParams(u, &udp.AnnounceRequest{UploadOnly: true}, AnnounceOpt{})
	if got := u.Query().Get("event"); got != "paused" {
		t.Fatalf("event=%q, want %q: a partial seed MUST announce event=paused (BEP 21)", got, "paused")
	}
}

// The regular interval announce carries no event at all. A partial seed must
// still say paused on it — BEP 21 says "every announce", precisely so a backup
// tracker can recover the swarm state without stored state.
func TestPartialSeedSaysPausedOnTheRegularAnnounce(t *testing.T) {
	u := &url.URL{Path: "/announce"}
	setAnnounceParams(u, &udp.AnnounceRequest{Event: shared.None, UploadOnly: true}, AnnounceOpt{})
	if got := u.Query().Get("event"); got != "paused" {
		t.Fatalf("event=%q on the regular announce, want paused (BEP 21 'every announce')", got)
	}
}

// A peer that is not a partial seed must not say paused. Announcing paused
// while we still want bytes tells the tracker to stop counting us as a
// downloader, which is the opposite of the truth.
func TestOrdinaryPeerDoesNotAnnouncePaused(t *testing.T) {
	u := &url.URL{Path: "/announce"}
	setAnnounceParams(u, &udp.AnnounceRequest{Event: shared.Started}, AnnounceOpt{})
	if got := u.Query().Get("event"); got != "started" {
		t.Fatalf("event=%q, want started for a peer that is not a partial seed", got)
	}
	if u.Query().Get("upload_only") != "" {
		t.Fatal("upload_only must be absent when we are not a partial seed")
	}
}

// completed and stopped report a one-time transition the tracker must not
// miss. BEP 21 does not override them.
func TestTransitionEventsSurvivePartialSeedState(t *testing.T) {
	for _, tc := range []struct {
		event shared.AnnounceEvent
		want  string
	}{
		{shared.Completed, "completed"},
		{shared.Stopped, "stopped"},
	} {
		u := &url.URL{Path: "/announce"}
		setAnnounceParams(u, &udp.AnnounceRequest{Event: tc.event, UploadOnly: true}, AnnounceOpt{})
		if got := u.Query().Get("event"); got != tc.want {
			t.Fatalf("event=%q, want %q: a real %s transition must not be replaced by paused",
				got, tc.want, tc.want)
		}
	}
}

// A partial seed is still INCOMPLETE. §17.1 forbids forcing Left=0, and
// nothing in this encoder may quietly zero it.
func TestPartialSeedDoesNotZeroLeft(t *testing.T) {
	u := &url.URL{Path: "/announce"}
	setAnnounceParams(u, &udp.AnnounceRequest{Left: 4096, UploadOnly: true}, AnnounceOpt{})
	if got := u.Query().Get("left"); got != "4096" {
		t.Fatalf("left=%q, want 4096: a partial seed is incomplete and must report real Left", got)
	}
}
