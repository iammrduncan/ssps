package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStoreCreatesSitesAndAggregatesCounters(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "ssps.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	firstID, err := store.CreateSite(ctx)
	if err != nil {
		t.Fatalf("create first site: %v", err)
	}
	secondID, err := store.CreateSite(ctx)
	if err != nil {
		t.Fatalf("create second site: %v", err)
	}
	if firstID <= 0 {
		t.Fatalf("first id = %d, want positive", firstID)
	}
	if secondID != firstID+1 {
		t.Fatalf("second id = %d, want %d", secondID, firstID+1)
	}

	err = store.ApplyVisitBatch(ctx, []VisitBatch{
		{SiteID: firstID, Hits: 3, VisitorIDs: []string{"a", "b", "a"}},
		{SiteID: firstID, Hits: 2, VisitorIDs: []string{"b", "c"}},
		{SiteID: 999999, Hits: 1, VisitorIDs: []string{"z"}},
	})
	if err != nil {
		t.Fatalf("apply visit batch: %v", err)
	}

	siteStats, err := store.SiteStats(ctx, firstID)
	if err != nil {
		t.Fatalf("site stats: %v", err)
	}
	if siteStats.SiteID != firstID {
		t.Fatalf("site id = %d, want %d", siteStats.SiteID, firstID)
	}
	if siteStats.TotalHits != 5 {
		t.Fatalf("total hits = %d, want 5", siteStats.TotalHits)
	}
	if siteStats.UniqueVisitors != 3 {
		t.Fatalf("unique visitors = %d, want 3", siteStats.UniqueVisitors)
	}

	networkStats, err := store.StoredStats(ctx)
	if err != nil {
		t.Fatalf("network stats: %v", err)
	}
	if networkStats.IDsCreated != 2 {
		t.Fatalf("ids created = %d, want 2", networkStats.IDsCreated)
	}
	if networkStats.TotalVisits != 6 {
		t.Fatalf("total visits = %d, want 6", networkStats.TotalVisits)
	}
}

func TestStoreAllowsReservedSiteZeroCounters(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "ssps.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.ApplyVisitBatch(ctx, []VisitBatch{
		{SiteID: 0, Hits: 2, VisitorIDs: []string{"self", "self"}},
	}); err != nil {
		t.Fatalf("apply reserved site batch: %v", err)
	}

	stats, err := store.SiteStats(ctx, 0)
	if err != nil {
		t.Fatalf("reserved site stats: %v", err)
	}
	if stats.SiteID != 0 {
		t.Fatalf("site id = %d, want 0", stats.SiteID)
	}
	if stats.TotalHits != 2 {
		t.Fatalf("total hits = %d, want 2", stats.TotalHits)
	}
	if stats.UniqueVisitors != 1 {
		t.Fatalf("unique visitors = %d, want 1", stats.UniqueVisitors)
	}

	createdID, err := store.CreateSite(ctx)
	if err != nil {
		t.Fatalf("create customer site: %v", err)
	}
	if createdID != 1 {
		t.Fatalf("first generated customer id = %d, want 1", createdID)
	}
}
