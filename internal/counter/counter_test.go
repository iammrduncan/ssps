package counter

import (
	"context"
	"errors"
	"testing"

	"github.com/josephduncan/ssps/internal/storage"
)

func TestAggregatorCompactsAndFlushesVisits(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	aggregator := NewAggregator(sink)

	aggregator.Record(10, "a")
	aggregator.Record(10, "a")
	aggregator.Record(10, "b")
	aggregator.Record(20, "z")

	pending := aggregator.Pending(10)
	if pending.Hits != 3 {
		t.Fatalf("pending hits = %d, want 3", pending.Hits)
	}
	if pending.UniqueVisitors != 2 {
		t.Fatalf("pending unique visitors = %d, want 2", pending.UniqueVisitors)
	}

	if err := aggregator.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if len(sink.batches) != 2 {
		t.Fatalf("flushed batches = %d, want 2", len(sink.batches))
	}
	assertBatch(t, sink.batches, 10, 3, []string{"a", "b"})
	assertBatch(t, sink.batches, 20, 1, []string{"z"})

	if pending := aggregator.Pending(10); pending.Hits != 0 || pending.UniqueVisitors != 0 {
		t.Fatalf("pending after flush = %+v, want zero", pending)
	}

	aggregator.Record(10, "c")
	if err := aggregator.Flush(context.Background()); err != nil {
		t.Fatalf("second flush: %v", err)
	}
	assertBatch(t, sink.batches[2:], 10, 1, []string{"c"})
}

func TestAggregatorRestoresEventsWhenFlushFails(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{err: errors.New("disk is tired")}
	aggregator := NewAggregator(sink)
	aggregator.Record(10, "a")

	if err := aggregator.Flush(context.Background()); err == nil {
		t.Fatal("flush err = nil, want error")
	}
	if pending := aggregator.Pending(10); pending.Hits != 1 || pending.UniqueVisitors != 1 {
		t.Fatalf("pending after failed flush = %+v, want restored event", pending)
	}
}

func TestAggregatorRecordsReservedSiteZero(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	aggregator := NewAggregator(sink)
	aggregator.Record(0, "self")
	aggregator.Record(0, "self")

	pending := aggregator.Pending(0)
	if pending.Hits != 2 {
		t.Fatalf("pending hits = %d, want 2", pending.Hits)
	}
	if pending.UniqueVisitors != 1 {
		t.Fatalf("pending unique visitors = %d, want 1", pending.UniqueVisitors)
	}

	if err := aggregator.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	assertBatch(t, sink.batches, 0, 2, []string{"self"})
}

func TestAggregatorCapsPendingMemory(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	aggregator := NewAggregator(sink)
	aggregator.maxPendingSites = 1
	aggregator.maxPendingVisitorsPerSite = 2

	aggregator.Record(10, "a")
	aggregator.Record(10, "b")
	aggregator.Record(10, "c")
	aggregator.Record(20, "z")

	pending := aggregator.Pending(10)
	if pending.Hits != 3 {
		t.Fatalf("admitted site hits = %d, want 3", pending.Hits)
	}
	if pending.UniqueVisitors != 2 {
		t.Fatalf("admitted site unique visitors = %d, want capped value 2", pending.UniqueVisitors)
	}
	if pending := aggregator.Pending(20); pending.Hits != 0 || pending.UniqueVisitors != 0 {
		t.Fatalf("overflow site pending = %+v, want zero", pending)
	}

	if err := aggregator.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if len(sink.batches) != 1 {
		t.Fatalf("flushed batches = %d, want 1 admitted site", len(sink.batches))
	}
	assertBatch(t, sink.batches, 10, 3, []string{"a", "b"})
}

type recordingSink struct {
	err     error
	batches []storage.VisitBatch
}

func (s *recordingSink) ApplyVisitBatch(_ context.Context, batches []storage.VisitBatch) error {
	if s.err != nil {
		return s.err
	}
	s.batches = append(s.batches, batches...)
	return nil
}

func assertBatch(t *testing.T, batches []storage.VisitBatch, siteID int64, hits int64, visitorIDs []string) {
	t.Helper()

	for _, batch := range batches {
		if batch.SiteID != siteID {
			continue
		}
		if batch.Hits != hits {
			t.Fatalf("site %d hits = %d, want %d", siteID, batch.Hits, hits)
		}
		got := map[string]bool{}
		for _, visitorID := range batch.VisitorIDs {
			got[visitorID] = true
		}
		for _, visitorID := range visitorIDs {
			if !got[visitorID] {
				t.Fatalf("site %d visitor ids = %v, want %q", siteID, batch.VisitorIDs, visitorID)
			}
		}
		return
	}
	t.Fatalf("missing batch for site %d in %+v", siteID, batches)
}
