package presence

import "sync"

type Snapshot struct {
	SiteID int64 `json:"siteId"`
	Live   int64 `json:"live"`
}

type Connection struct {
	id     int64
	siteID int64
}

type Hub struct {
	mu          sync.Mutex
	nextID      int64
	connections map[int64]Connection
	sites       map[int64]map[int64]chan Snapshot
}

func NewHub() *Hub {
	return &Hub{
		connections: make(map[int64]Connection),
		sites:       make(map[int64]map[int64]chan Snapshot),
	}
}

func (h *Hub) Register(siteID int64) (Connection, <-chan Snapshot) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.nextID++
	connection := Connection{id: h.nextID, siteID: siteID}
	updates := make(chan Snapshot, 8)
	h.connections[connection.id] = connection
	if h.sites[siteID] == nil {
		h.sites[siteID] = make(map[int64]chan Snapshot)
	}
	h.sites[siteID][connection.id] = updates
	return connection, updates
}

func (h *Hub) Unregister(connection Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.connections, connection.id)
	if subscribers, ok := h.sites[connection.siteID]; ok {
		if updates, ok := subscribers[connection.id]; ok {
			close(updates)
			delete(subscribers, connection.id)
		}
		if len(subscribers) == 0 {
			delete(h.sites, connection.siteID)
		}
	}
}

func (h *Hub) Broadcast(siteID int64) {
	h.mu.Lock()
	snapshot := Snapshot{SiteID: siteID, Live: int64(len(h.sites[siteID]))}
	subscribers := make([]chan Snapshot, 0, len(h.sites[siteID]))
	for _, updates := range h.sites[siteID] {
		subscribers = append(subscribers, updates)
	}
	h.mu.Unlock()

	for _, updates := range subscribers {
		select {
		case updates <- snapshot:
		default:
			select {
			case <-updates:
			default:
			}
			select {
			case updates <- snapshot:
			default:
			}
		}
	}
}

func (h *Hub) SiteLive(siteID int64) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return int64(len(h.sites[siteID]))
}

func (h *Hub) LiveUsers() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return int64(len(h.connections))
}

func (h *Hub) ActiveSites() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return int64(len(h.sites))
}
