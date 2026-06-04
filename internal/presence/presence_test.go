package presence

import "testing"

func TestHubTracksLiveUsersAndBroadcastsSiteSnapshots(t *testing.T) {
	t.Parallel()

	hub := NewHub()

	first, firstUpdates := hub.Register(42)
	second, secondUpdates := hub.Register(42)
	third, _ := hub.Register(7)

	if got := hub.SiteLive(42); got != 2 {
		t.Fatalf("site 42 live = %d, want 2", got)
	}
	if got := hub.LiveUsers(); got != 3 {
		t.Fatalf("live users = %d, want 3", got)
	}
	if got := hub.ActiveSites(); got != 2 {
		t.Fatalf("active sites = %d, want 2", got)
	}

	hub.Broadcast(42)
	assertSnapshot(t, firstUpdates, 42, 2)
	assertSnapshot(t, secondUpdates, 42, 2)

	hub.Unregister(first)
	if got := hub.SiteLive(42); got != 1 {
		t.Fatalf("site 42 live after unregister = %d, want 1", got)
	}
	hub.Broadcast(42)
	assertSnapshot(t, secondUpdates, 42, 1)

	hub.Unregister(second)
	hub.Unregister(third)
	if got := hub.LiveUsers(); got != 0 {
		t.Fatalf("live users after cleanup = %d, want 0", got)
	}
	if got := hub.ActiveSites(); got != 0 {
		t.Fatalf("active sites after cleanup = %d, want 0", got)
	}
}

func assertSnapshot(t *testing.T, updates <-chan Snapshot, siteID int64, live int64) {
	t.Helper()

	select {
	case snapshot := <-updates:
		if snapshot.SiteID != siteID {
			t.Fatalf("snapshot site id = %d, want %d", snapshot.SiteID, siteID)
		}
		if snapshot.Live != live {
			t.Fatalf("snapshot live = %d, want %d", snapshot.Live, live)
		}
	default:
		t.Fatalf("missing snapshot for site %d", siteID)
	}
}
