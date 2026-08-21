# Restore tracker announces when re-adding a torrent

Re-adding a dropped torrent with the same info hash can reuse retained regular tracker-announcer state. The retained key remains associated with the old `Torrent`, and `addKey` returns before it replaces the weak torrent reference or links the announce state to the replacement torrent. No fresh `Started` announce is then scheduled, so peer discovery and metadata retrieval stop.

On unmodified v1.61.0, the regression reproduces with `before_peers=1`, `tracker_announces_before_drop=1`, `tracker_announces_after_drop=2`, `stopped_announces_after_drop=1`, `after_readd_peers=0`, `tracker_announces_after_readd=2`, and `metadata_resolved=false`.

This change rebinds retained keys to the new `Torrent`, links and resets the announce lifecycle while preserving in-flight concurrency fields, and schedules a fresh `Started` announce after a prior `Stopped` event. It also removes announce records when no event remains.

`TestTrackerDropAndReAddSameInfoHash` holds the prior `Stopped` announce in flight while re-adding the same info hash. It verifies a new `Started` announce, peer discovery, metadata resolution, and final dispatcher-state cleanup. With the fix, the result is `after_readd_peers=1`, `tracker_announces_after_readd=3`, `tracker_started_announces_after_readd=2`, and `metadata_resolved=true`.

Related: issue #1048 is closed. The early return remains on master, and open, unmerged PR #1051—which issue #1048 was expected to be addressed by—also touches this lifecycle as part of a broader change.
