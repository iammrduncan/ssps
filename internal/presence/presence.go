package presence

import "sync"

type Snapshot struct {
	SiteID  int64  `json:"siteId"`
	Live    int64  `json:"live"`
	Version uint64 `json:"version"`
}

type Connection struct {
	id     int64
	siteID int64
}

type Hub struct {
	mu     sync.Mutex
	nextID int64

	connections map[int64]Connection
	siteLive    map[int64]int64
	siteVersion map[int64]uint64
}

func NewHub() *Hub {
	return &Hub{
		connections: make(map[int64]Connection),
		siteLive:    make(map[int64]int64),
		siteVersion: make(map[int64]uint64),
	}
}

func (h *Hub) Register(siteID int64) Connection {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.nextID++
	connection := Connection{id: h.nextID, siteID: siteID}
	h.connections[connection.id] = connection
	h.siteLive[siteID]++
	h.siteVersion[siteID]++
	return connection
}

func (h *Hub) Unregister(connection Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.connections[connection.id]; !ok {
		return
	}
	delete(h.connections, connection.id)
	if live := h.siteLive[connection.siteID]; live <= 1 {
		delete(h.siteLive, connection.siteID)
	} else {
		h.siteLive[connection.siteID] = live - 1
	}
	h.siteVersion[connection.siteID]++
}

func (h *Hub) SiteSnapshot(siteID int64) Snapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return Snapshot{SiteID: siteID, Live: h.siteLive[siteID], Version: h.siteVersion[siteID]}
}

func (h *Hub) SiteLive(siteID int64) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.siteLive[siteID]
}

func (h *Hub) LiveUsers() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return int64(len(h.connections))
}

func (h *Hub) ActiveSites() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return int64(len(h.siteLive))
}
