package counter

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/josephduncan/ssps/internal/storage"
)

type Sink interface {
	ApplyVisitBatch(context.Context, []storage.VisitBatch) error
}

type PendingStats struct {
	Hits           int64
	UniqueVisitors int64
}

const (
	defaultMaxPendingSites           = 100000
	defaultMaxPendingVisitorsPerSite = 500000
)

type Aggregator struct {
	mu                        sync.Mutex
	sink                      Sink
	pending                   map[int64]*siteEvents
	maxPendingSites           int
	maxPendingVisitorsPerSite int
}

type siteEvents struct {
	hits     int64
	visitors map[string]bool
}

func NewAggregator(sink Sink) *Aggregator {
	return &Aggregator{
		sink:                      sink,
		pending:                   make(map[int64]*siteEvents),
		maxPendingSites:           defaultMaxPendingSites,
		maxPendingVisitorsPerSite: defaultMaxPendingVisitorsPerSite,
	}
}

func (a *Aggregator) Record(siteID int64, visitorID string) {
	if siteID < 0 {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	events := a.pending[siteID]
	if events == nil {
		if a.maxPendingSites > 0 && len(a.pending) >= a.maxPendingSites {
			return
		}
		events = &siteEvents{visitors: make(map[string]bool)}
		a.pending[siteID] = events
	}
	events.hits++
	if visitorID != "" {
		if !events.visitors[visitorID] && a.maxPendingVisitorsPerSite > 0 && len(events.visitors) >= a.maxPendingVisitorsPerSite {
			return
		}
		events.visitors[visitorID] = true
	}
}

func (a *Aggregator) Pending(siteID int64) PendingStats {
	a.mu.Lock()
	defer a.mu.Unlock()

	events := a.pending[siteID]
	if events == nil {
		return PendingStats{}
	}
	return PendingStats{Hits: events.hits, UniqueVisitors: int64(len(events.visitors))}
}

func (a *Aggregator) TotalPendingHits() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()

	var total int64
	for _, events := range a.pending {
		total += events.hits
	}
	return total
}

func (a *Aggregator) Flush(ctx context.Context) error {
	batches := a.drain()
	if len(batches) == 0 {
		return nil
	}
	if err := a.sink.ApplyVisitBatch(ctx, batches); err != nil {
		a.restore(batches)
		return err
	}
	return nil
}

func (a *Aggregator) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Minute
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if err := a.Flush(context.Background()); err != nil {
				slog.Error("flush visits during shutdown", "error", err)
			}
			return
		case <-ticker.C:
			if err := a.Flush(ctx); err != nil {
				slog.Error("flush visits", "error", err)
			}
		}
	}
}

func (a *Aggregator) drain() []storage.VisitBatch {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.pending) == 0 {
		return nil
	}
	batches := make([]storage.VisitBatch, 0, len(a.pending))
	for siteID, events := range a.pending {
		visitorIDs := make([]string, 0, len(events.visitors))
		for visitorID := range events.visitors {
			visitorIDs = append(visitorIDs, visitorID)
		}
		sort.Strings(visitorIDs)
		batches = append(batches, storage.VisitBatch{
			SiteID:     siteID,
			Hits:       events.hits,
			VisitorIDs: visitorIDs,
		})
	}
	a.pending = make(map[int64]*siteEvents)
	return batches
}

func (a *Aggregator) restore(batches []storage.VisitBatch) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, batch := range batches {
		events := a.pending[batch.SiteID]
		if events == nil {
			events = &siteEvents{visitors: make(map[string]bool)}
			a.pending[batch.SiteID] = events
		}
		events.hits += batch.Hits
		for _, visitorID := range batch.VisitorIDs {
			if visitorID != "" {
				events.visitors[visitorID] = true
			}
		}
	}
}
