package presence

import "testing"

func TestHubTracksLiveUsersAndVersionedSiteSnapshots(t *testing.T) {
	t.Parallel()

	hub := NewHub()

	first := hub.Register(42)
	second := hub.Register(42)
	third := hub.Register(7)

	if got := hub.SiteLive(42); got != 2 {
		t.Fatalf("site 42 live = %d, want 2", got)
	}
	snapshot := hub.SiteSnapshot(42)
	if snapshot.SiteID != 42 {
		t.Fatalf("snapshot site id = %d, want 42", snapshot.SiteID)
	}
	if snapshot.Live != 2 {
		t.Fatalf("snapshot live = %d, want 2", snapshot.Live)
	}
	if snapshot.Version != 2 {
		t.Fatalf("snapshot version = %d, want 2 registrations", snapshot.Version)
	}
	if got := hub.LiveUsers(); got != 3 {
		t.Fatalf("live users = %d, want 3", got)
	}
	if got := hub.ActiveSites(); got != 2 {
		t.Fatalf("active sites = %d, want 2", got)
	}

	hub.Unregister(first)
	if got := hub.SiteLive(42); got != 1 {
		t.Fatalf("site 42 live after unregister = %d, want 1", got)
	}
	snapshot = hub.SiteSnapshot(42)
	if snapshot.Live != 1 {
		t.Fatalf("snapshot live after unregister = %d, want 1", snapshot.Live)
	}
	if snapshot.Version != 3 {
		t.Fatalf("snapshot version after unregister = %d, want 3", snapshot.Version)
	}

	hub.Unregister(second)
	hub.Unregister(third)
	if got := hub.LiveUsers(); got != 0 {
		t.Fatalf("live users after cleanup = %d, want 0", got)
	}
	if got := hub.ActiveSites(); got != 0 {
		t.Fatalf("active sites after cleanup = %d, want 0", got)
	}
}
