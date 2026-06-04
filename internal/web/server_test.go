package web

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/josephduncan/ssps/internal/counter"
	"github.com/josephduncan/ssps/internal/presence"
	"github.com/josephduncan/ssps/internal/storage"
)

func TestServerRoutesGenerateScriptAndStats(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	assertStatus(t, server, "/healthz", http.StatusOK, "ok")
	home := get(t, server, "/")
	if home.Code != http.StatusOK {
		t.Fatalf("/ status = %d, want 200", home.Code)
	}
	if !strings.Contains(home.Body.String(), "Stupid Simple Presence Service") {
		t.Fatalf("/ body missing service name")
	}
	if !strings.Contains(home.Body.String(), "The top stats are network-wide") {
		t.Fatalf("/ body missing network-wide stats explanation")
	}
	if !strings.Contains(home.Body.String(), `<span class="value" data-ssps-network-live-users>`) ||
		!strings.Contains(home.Body.String(), `<span class="value" data-ssps-network-total-visits>`) {
		t.Fatalf("/ body missing live-updating network stats")
	}
	if !strings.Contains(home.Body.String(), `<span id="ssps-live-count">0</span>`) ||
		!strings.Contains(home.Body.String(), `<span id="ssps-visit-count">0</span>`) ||
		!strings.Contains(home.Body.String(), `<span id="ssps-unique-visit-count">0</span>`) {
		t.Fatalf("/ body missing rendered ssps-prefixed example spans")
	}
	if !strings.Contains(home.Body.String(), `src="/ssps.js" data-site-id="0" data-network-stats`) {
		t.Fatalf("/ body missing self-use script with network stats mode")
	}
	if strings.Contains(home.Body.String(), `id="live-count"`) || strings.Contains(home.Body.String(), `id="visit-count"`) || strings.Contains(home.Body.String(), `id="unique-visit-count"`) {
		t.Fatalf("/ body contains unprefixed counter span id")
	}

	generate := get(t, server, "/generate")
	if generate.Code != http.StatusOK {
		t.Fatalf("/generate status = %d, want 200", generate.Code)
	}
	if !strings.Contains(generate.Body.String(), `data-site-id=&#34;1&#34;`) {
		t.Fatalf("/generate body missing site id snippet: %s", generate.Body.String())
	}

	script := get(t, server, "/ssps.js")
	if script.Code != http.StatusOK {
		t.Fatalf("/ssps.js status = %d, want 200", script.Code)
	}
	if contentType := script.Header().Get("Content-Type"); !strings.Contains(contentType, "javascript") {
		t.Fatalf("script content type = %q, want javascript", contentType)
	}
	if !strings.Contains(script.Body.String(), "window.SSPS") {
		t.Fatalf("script body missing window.SSPS API")
	}
	if !strings.Contains(script.Body.String(), "ssps-live-count") {
		t.Fatalf("script body missing live count updater")
	}
	if !strings.Contains(script.Body.String(), "ssps-visit-count") || !strings.Contains(script.Body.String(), "ssps-unique-visit-count") {
		t.Fatalf("script body missing ssps-prefixed counter IDs")
	}
	if strings.Contains(script.Body.String(), "#visit-count") || strings.Contains(script.Body.String(), "#unique-visit-count") {
		t.Fatalf("script body contains unprefixed counter ID selectors")
	}
	if !strings.Contains(script.Body.String(), "data-ssps-network-total-visits") ||
		!strings.Contains(script.Body.String(), "/api/stats") {
		t.Fatalf("script body missing network stats updater")
	}

	stats := get(t, server, "/api/stats")
	if stats.Code != http.StatusOK {
		t.Fatalf("/api/stats status = %d, want 200", stats.Code)
	}
	var network NetworkStats
	if err := json.Unmarshal(stats.Body.Bytes(), &network); err != nil {
		t.Fatalf("decode network stats: %v", err)
	}
	if network.IDsCreated != 1 {
		t.Fatalf("ids created = %d, want 1", network.IDsCreated)
	}

	siteStats := get(t, server, "/api/sites/1/stats")
	if siteStats.Code != http.StatusOK {
		t.Fatalf("/api/sites/1/stats status = %d, want 200", siteStats.Code)
	}
	var site SiteStats
	if err := json.Unmarshal(siteStats.Body.Bytes(), &site); err != nil {
		t.Fatalf("decode site stats: %v", err)
	}
	if site.SiteID != 1 {
		t.Fatalf("site id = %d, want 1", site.SiteID)
	}
}

func TestWebSocketConnectionCountsLiveUserAndVisit(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	httpServer := httptest.NewServer(server.Handler)
	defer httpServer.Close()

	conn, reader, err := dialWebSocket(httpServer.URL, "/ws?site-id=0&visitor-id=abc")
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	payload, err := readServerTextFrame(reader)
	if err != nil {
		t.Fatalf("read websocket payload: %v", err)
	}
	var site SiteStats
	if err := json.Unmarshal(payload, &site); err != nil {
		t.Fatalf("decode websocket payload: %v", err)
	}
	if site.SiteID != 0 {
		t.Fatalf("site id = %d, want 0", site.SiteID)
	}
	if site.Live != 1 {
		t.Fatalf("live = %d, want 1", site.Live)
	}
	if site.TotalHits != 1 {
		t.Fatalf("total hits = %d, want 1", site.TotalHits)
	}
	if site.UniqueVisitors != 1 {
		t.Fatalf("unique visitors = %d, want 1", site.UniqueVisitors)
	}

	stats := get(t, server, "/api/stats")
	if stats.Code != http.StatusOK {
		t.Fatalf("/api/stats status = %d, want 200", stats.Code)
	}
	var network NetworkStats
	if err := json.Unmarshal(stats.Body.Bytes(), &network); err != nil {
		t.Fatalf("decode network stats: %v", err)
	}
	if network.TotalVisits != 1 {
		t.Fatalf("network total visits = %d, want 1 pending visit included", network.TotalVisits)
	}

	siteStats := get(t, server, "/api/sites/0/stats")
	if siteStats.Code != http.StatusOK {
		t.Fatalf("/api/sites/0/stats status = %d, want 200", siteStats.Code)
	}
}

func TestReservedSiteZeroReportsOwnLiveWhileNetworkStatsAggregate(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	httpServer := httptest.NewServer(server.Handler)
	defer httpServer.Close()

	siteZeroConn, siteZeroReader, err := dialWebSocket(httpServer.URL, "/ws?site-id=0&visitor-id=self")
	if err != nil {
		t.Fatalf("dial site zero websocket: %v", err)
	}
	defer siteZeroConn.Close()
	if _, err := readServerTextFrame(siteZeroReader); err != nil {
		t.Fatalf("read site zero initial payload: %v", err)
	}
	if _, err := readServerTextFrame(siteZeroReader); err != nil {
		t.Fatalf("read site zero self-broadcast payload: %v", err)
	}

	siteOneConn, _, err := dialWebSocket(httpServer.URL, "/ws?site-id=1&visitor-id=customer")
	if err != nil {
		t.Fatalf("dial site one websocket: %v", err)
	}
	defer siteOneConn.Close()

	payload, err := readServerTextFrame(siteZeroReader)
	if err != nil {
		t.Fatalf("read site zero update payload: %v", err)
	}
	var site SiteStats
	if err := json.Unmarshal(payload, &site); err != nil {
		t.Fatalf("decode site zero update payload: %v", err)
	}
	if site.SiteID != 0 {
		t.Fatalf("site id = %d, want 0", site.SiteID)
	}
	if site.Live != 1 {
		t.Fatalf("reserved site live = %d, want only site zero live users 1", site.Live)
	}

	stats := get(t, server, "/api/stats")
	if stats.Code != http.StatusOK {
		t.Fatalf("/api/stats status = %d, want 200", stats.Code)
	}
	var network NetworkStats
	if err := json.Unmarshal(stats.Body.Bytes(), &network); err != nil {
		t.Fatalf("decode network stats: %v", err)
	}
	if network.LiveUsers != 2 {
		t.Fatalf("network live users = %d, want 2", network.LiveUsers)
	}
	if network.ActiveSites != 2 {
		t.Fatalf("network active sites = %d, want 2", network.ActiveSites)
	}
	if network.TotalVisits != 2 {
		t.Fatalf("network total visits = %d, want site zero plus site one visits", network.TotalVisits)
	}
}

type testServer struct {
	Handler http.Handler
}

func newTestServer(t *testing.T) testServer {
	t.Helper()

	store, err := storage.Open(filepath.Join(t.TempDir(), "ssps.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	hub := presence.NewHub()
	aggregator := counter.NewAggregator(store)
	return testServer{Handler: NewServer(store, hub, aggregator)}
}

func get(t *testing.T, server testServer, path string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	server.Handler.ServeHTTP(rec, req)
	return rec
}

func assertStatus(t *testing.T, server testServer, path string, status int, bodyContains string) {
	t.Helper()

	rec := get(t, server, path)
	if rec.Code != status {
		t.Fatalf("%s status = %d, want %d", path, rec.Code, status)
	}
	if !strings.Contains(rec.Body.String(), bodyContains) {
		t.Fatalf("%s body = %q, want containing %q", path, rec.Body.String(), bodyContains)
	}
}

func dialWebSocket(serverURL string, path string) (net.Conn, *bufio.Reader, error) {
	parsed, err := url.Parse(serverURL)
	if err != nil {
		return nil, nil, err
	}

	conn, err := net.Dial("tcp", parsed.Host)
	if err != nil {
		return nil, nil, err
	}

	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		conn.Close()
		return nil, nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	request := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\n\r\n", path, parsed.Host, key)
	if _, err := conn.Write([]byte(request)); err != nil {
		conn.Close()
		return nil, nil, err
	}

	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	if !strings.Contains(status, "101") {
		conn.Close()
		return nil, nil, fmt.Errorf("websocket status line = %q", status)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, nil, err
		}
		if line == "\r\n" {
			break
		}
	}
	return conn, reader, nil
}

func readServerTextFrame(reader *bufio.Reader) ([]byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, err
	}
	if opcode := header[0] & 0x0F; opcode != 0x1 {
		return nil, fmt.Errorf("opcode = %d, want text", opcode)
	}
	length := uint64(header[1] & 0x7F)
	switch length {
	case 126:
		extended := make([]byte, 2)
		if _, err := io.ReadFull(reader, extended); err != nil {
			return nil, err
		}
		length = uint64(binary.BigEndian.Uint16(extended))
	case 127:
		extended := make([]byte, 8)
		if _, err := io.ReadFull(reader, extended); err != nil {
			return nil, err
		}
		length = binary.BigEndian.Uint64(extended)
	}
	payload := make([]byte, length)
	_, err := io.ReadFull(reader, payload)
	return payload, err
}
