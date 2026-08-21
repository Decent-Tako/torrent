package torrent

// DataUploadDisallowed reports whether DisallowDataUpload is currently in
// effect. PeerConn.uploadAllowed consults the same flag, so this is the
// authoritative answer to "will this torrent serve peer REQUESTs".
//
// Added for Mariotte issue #230: a paused torrent must not seed, and the test
// that pins it needs to read the flag it sets.
func (t *Torrent) DataUploadDisallowed() bool {
	t.cl.rLock()
	defer t.cl.rUnlock()
	return t.dataUploadDisallowed
}
