package web

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/josephduncan/ssps/internal/counter"
	"github.com/josephduncan/ssps/internal/presence"
	"github.com/josephduncan/ssps/internal/storage"
)

type store interface {
	CreateSite(context.Context) (int64, error)
	SiteStats(context.Context, int64) (storage.SiteStats, error)
	StoredStats(context.Context) (storage.StoredStats, error)
}

type Server struct {
	store   store
	hub     *presence.Hub
	counter *counter.Aggregator
	mux     *http.ServeMux
}

type SiteStats struct {
	SiteID         int64 `json:"siteId"`
	Live           int64 `json:"live"`
	TotalHits      int64 `json:"totalHits"`
	UniqueVisitors int64 `json:"uniqueVisitors"`
}

type NetworkStats struct {
	IDsCreated  int64 `json:"idsCreated"`
	LiveUsers   int64 `json:"liveUsers"`
	ActiveSites int64 `json:"activeSites"`
	TotalVisits int64 `json:"totalVisits"`
}

func NewServer(store store, hub *presence.Hub, aggregator *counter.Aggregator) http.Handler {
	server := &Server{store: store, hub: hub, counter: aggregator, mux: http.NewServeMux()}
	server.routes()
	return server
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.handleHome)
	s.mux.HandleFunc("GET /generate", s.handleGenerate)
	s.mux.HandleFunc("GET /ssps.js", s.handleScript)
	s.mux.HandleFunc("GET /api/stats", s.handleNetworkStats)
	s.mux.HandleFunc("GET /api/sites/", s.handleSiteStats)
	s.mux.HandleFunc("GET /ws", s.handleWebSocket)
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ok")
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	stats, err := s.networkStats(r.Context())
	if err != nil {
		http.Error(w, "could not read stats", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, renderHome(stats))
}

func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.store.CreateSite(r.Context())
	if err != nil {
		http.Error(w, "could not create site id", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, renderGenerate(siteID, absoluteScriptURL(r)))
}

func (s *Server) handleScript(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	fmt.Fprint(w, scriptJS())
}

func (s *Server) handleNetworkStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.networkStats(r.Context())
	if err != nil {
		http.Error(w, "could not read stats", http.StatusInternalServerError)
		return
	}
	writeJSON(w, stats)
}

func (s *Server) handleSiteStats(w http.ResponseWriter, r *http.Request) {
	siteID, ok := parseSiteStatsPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	stats, err := s.siteStats(r.Context(), siteID)
	if err != nil {
		http.Error(w, "could not read site stats", http.StatusInternalServerError)
		return
	}
	writeJSON(w, stats)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	siteID, err := parseSiteID(r.URL.Query().Get("site-id"))
	if err != nil {
		http.Error(w, "site-id must be a non-negative number", http.StatusBadRequest)
		return
	}
	visitorID := r.URL.Query().Get("visitor-id")
	if visitorID == "" {
		visitorID = fmt.Sprintf("anonymous-%d", time.Now().UnixNano())
	}

	conn, err := acceptWebSocket(w, r)
	if err != nil {
		slog.Debug("accept websocket", "error", err)
		return
	}
	defer conn.Close()

	connection, updates := s.hub.Register(siteID)
	defer func() {
		s.hub.Unregister(connection)
		s.hub.Broadcast(siteID)
	}()

	s.counter.Record(siteID, visitorID)
	if err := s.writeSiteStats(r.Context(), conn, siteID); err != nil {
		return
	}
	s.hub.Broadcast(siteID)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if err := conn.ReadLoop(r.Context()); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-done:
			return
		case _, ok := <-updates:
			if !ok {
				return
			}
			if err := s.writeSiteStats(r.Context(), conn, siteID); err != nil {
				return
			}
		}
	}
}

func (s *Server) siteStats(ctx context.Context, siteID int64) (SiteStats, error) {
	stored, err := s.store.SiteStats(ctx, siteID)
	if err != nil {
		return SiteStats{}, err
	}
	pending := s.counter.Pending(siteID)
	return SiteStats{
		SiteID:         siteID,
		Live:           s.hub.SiteLive(siteID),
		TotalHits:      stored.TotalHits + pending.Hits,
		UniqueVisitors: stored.UniqueVisitors + pending.UniqueVisitors,
	}, nil
}

func (s *Server) networkStats(ctx context.Context) (NetworkStats, error) {
	stored, err := s.store.StoredStats(ctx)
	if err != nil {
		return NetworkStats{}, err
	}
	return NetworkStats{
		IDsCreated:  stored.IDsCreated,
		LiveUsers:   s.hub.LiveUsers(),
		ActiveSites: s.hub.ActiveSites(),
		TotalVisits: stored.TotalVisits + s.counter.TotalPendingHits(),
	}, nil
}

func (s *Server) writeSiteStats(ctx context.Context, conn *wsConn, siteID int64) error {
	stats, err := s.siteStats(ctx, siteID)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(stats)
	if err != nil {
		return err
	}
	return conn.WriteText(ctx, payload)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		slog.Error("write json", "error", err)
	}
}

func parseSiteStatsPath(path string) (int64, bool) {
	const prefix = "/api/sites/"
	const suffix = "/stats"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return 0, false
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	siteID, err := parseSiteID(raw)
	return siteID, err == nil
}

func parseSiteID(raw string) (int64, error) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("invalid site id %q", raw)
	}
	return value, nil
}

func absoluteScriptURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil && (strings.HasPrefix(r.Host, "localhost") || strings.HasPrefix(r.Host, "127.0.0.1") || strings.HasPrefix(r.Host, "[::1]")) {
		scheme = "http"
	}
	if forwardedProto := r.Header.Get("X-Forwarded-Proto"); forwardedProto == "http" || forwardedProto == "https" {
		scheme = forwardedProto
	}
	host := r.Host
	if host == "" {
		host = "usessps.com"
	}
	return scheme + "://" + host + "/ssps.js"
}

type wsConn struct {
	conn   net.Conn
	reader *bufio.Reader
	mu     sync.Mutex
}

func acceptWebSocket(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	if !headerContains(r.Header, "Connection", "upgrade") || !headerContains(r.Header, "Upgrade", "websocket") {
		http.Error(w, "websocket upgrade required", http.StatusBadRequest)
		return nil, fmt.Errorf("websocket upgrade required")
	}

	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "missing websocket key", http.StatusBadRequest)
		return nil, fmt.Errorf("missing websocket key")
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "websocket unsupported", http.StatusInternalServerError)
		return nil, fmt.Errorf("response writer does not support hijacking")
	}

	netConn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}

	accept := websocketAccept(key)
	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := rw.WriteString(response); err != nil {
		netConn.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		netConn.Close()
		return nil, err
	}

	return &wsConn{conn: netConn, reader: rw.Reader}, nil
}

func (c *wsConn) ReadLoop(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		opcode, payload, err := c.readFrame()
		if err != nil {
			return err
		}
		switch opcode {
		case 0x8:
			return io.EOF
		case 0x9:
			_ = c.writeFrame(0xA, payload)
		}
	}
}

func (c *wsConn) WriteText(ctx context.Context, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.writeFrame(0x1, payload)
}

func (c *wsConn) Close() error {
	_ = c.writeFrame(0x8, nil)
	return c.conn.Close()
}

func (c *wsConn) readFrame() (byte, []byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(c.reader, header); err != nil {
		return 0, nil, err
	}

	opcode := header[0] & 0x0F
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7F)
	switch length {
	case 126:
		extended := make([]byte, 2)
		if _, err := io.ReadFull(c.reader, extended); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(extended))
	case 127:
		extended := make([]byte, 8)
		if _, err := io.ReadFull(c.reader, extended); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(extended)
	}
	if length > 1<<20 {
		return 0, nil, fmt.Errorf("websocket frame too large: %d", length)
	}

	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.reader, mask[:]); err != nil {
			return 0, nil, err
		}
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for idx := range payload {
			payload[idx] ^= mask[idx%4]
		}
	}
	return opcode, payload, nil
}

func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	header := []byte{0x80 | opcode}
	switch {
	case len(payload) < 126:
		header = append(header, byte(len(payload)))
	case len(payload) <= 65535:
		header = append(header, 126, byte(len(payload)>>8), byte(len(payload)))
	default:
		header = append(header, 127)
		length := make([]byte, 8)
		binary.BigEndian.PutUint64(length, uint64(len(payload)))
		header = append(header, length...)
	}

	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := c.conn.Write(payload)
	return err
}

func websocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func headerContains(headers http.Header, key string, value string) bool {
	for _, part := range strings.Split(headers.Get(key), ",") {
		if strings.EqualFold(strings.TrimSpace(part), value) {
			return true
		}
	}
	return false
}
