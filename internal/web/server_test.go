package web

import (
	"bufio"
	"bytes"
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
	"strconv"
	"strings"
	"testing"

	"github.com/josephduncan/ssps/internal/counter"
	"github.com/josephduncan/ssps/internal/presence"
	"github.com/josephduncan/ssps/internal/storage"
)

const testWebSocketKey = "dGhlIHNhbXBsZSBub25jZQ=="

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
	if !strings.Contains(home.Body.String(), `href="https://github.com/iammrduncan/ssps"`) {
		t.Fatalf("/ body missing source code link")
	}
	if !strings.Contains(home.Body.String(), `<link rel="icon" href="/favicon.svg" type="image/svg+xml">`) {
		t.Fatalf("/ body missing favicon link")
	}
	if !strings.Contains(home.Body.String(), `Made with &lt;3 by <a href="https://iammrduncan.com">Shannon</a>`) {
		t.Fatalf("/ body missing Shannon credit")
	}
	if !strings.Contains(home.Body.String(), `<!-- Fathom - beautiful, simple website analytics -->
<script src="https://cdn.usefathom.com/script.js" data-site="TETCAXTQ" defer></script>
<!-- / Fathom -->`) {
		t.Fatalf("/ body missing Fathom analytics snippet")
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

	favicon := get(t, server, "/favicon.svg")
	if favicon.Code != http.StatusOK {
		t.Fatalf("/favicon.svg status = %d, want 200", favicon.Code)
	}
	if contentType := favicon.Header().Get("Content-Type"); !strings.Contains(contentType, "image/svg+xml") {
		t.Fatalf("favicon content type = %q, want image/svg+xml", contentType)
	}
	if !strings.Contains(favicon.Body.String(), "<svg") || !strings.Contains(favicon.Body.String(), "SSPS") {
		t.Fatalf("favicon body missing svg mark")
	}

	generate := get(t, server, "/generate")
	if generate.Code != http.StatusOK {
		t.Fatalf("/generate status = %d, want 200", generate.Code)
	}
	if !strings.Contains(generate.Body.String(), `data-site-id=&#34;1&#34;`) {
		t.Fatalf("/generate body missing site id snippet: %s", generate.Body.String())
	}
	if !strings.Contains(generate.Body.String(), `src="https://cdn.usefathom.com/script.js" data-site="TETCAXTQ" defer`) {
		t.Fatalf("/generate body missing Fathom analytics snippet")
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
	if !strings.Contains(script.Body.String(), "reconnectDelay") ||
		!strings.Contains(script.Body.String(), "Math.min(reconnectDelay * 2, 30000)") {
		t.Fatalf("script body missing reconnect backoff")
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

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	rec := get(t, server, "/")

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q, want no-referrer", got)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("Content-Security-Policy = %q, want restrictive default and frame ancestors", csp)
	}
	if !strings.Contains(csp, "script-src 'self' https://cdn.usefathom.com") {
		t.Fatalf("Content-Security-Policy = %q, want Fathom script allowance", csp)
	}
	if !strings.Contains(csp, "img-src 'self' https://cdn.usefathom.com") {
		t.Fatalf("Content-Security-Policy = %q, want Fathom image/beacon allowance", csp)
	}
}

func TestGenerateRateLimit(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	for idx := 0; idx < generateRequestsPerWindow; idx++ {
		rec := get(t, server, "/generate")
		if rec.Code != http.StatusOK {
			t.Fatalf("generate request %d status = %d, want 200", idx+1, rec.Code)
		}
	}

	rec := get(t, server, "/generate")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("generate over limit status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

func TestWebSocketRejectsInvalidHandshake(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	httpServer := httptest.NewServer(server.Handler)
	defer httpServer.Close()

	missingVersion := websocketHandshakeStatus(t, httpServer.URL, "/ws?site-id=0&visitor-id=abc", map[string]string{
		"Sec-WebSocket-Key": testWebSocketKey,
	})
	if missingVersion != http.StatusBadRequest {
		t.Fatalf("missing websocket version status = %d, want %d", missingVersion, http.StatusBadRequest)
	}

	invalidKey := websocketHandshakeStatus(t, httpServer.URL, "/ws?site-id=0&visitor-id=abc", map[string]string{
		"Sec-WebSocket-Version": "13",
		"Sec-WebSocket-Key":     "not-a-valid-key",
	})
	if invalidKey != http.StatusBadRequest {
		t.Fatalf("invalid websocket key status = %d, want %d", invalidKey, http.StatusBadRequest)
	}

	longVisitorID := strings.Repeat("a", 129)
	longVisitor := websocketHandshakeStatus(t, httpServer.URL, "/ws?site-id=0&visitor-id="+longVisitorID, map[string]string{
		"Sec-WebSocket-Version": "13",
		"Sec-WebSocket-Key":     testWebSocketKey,
	})
	if longVisitor != http.StatusBadRequest {
		t.Fatalf("long visitor id websocket status = %d, want %d", longVisitor, http.StatusBadRequest)
	}
}

func TestReadFrameRejectsUnmaskedClientFrames(t *testing.T) {
	t.Parallel()

	conn := &wsConn{reader: bufio.NewReader(bytes.NewReader([]byte{0x89, 0x00}))}
	if _, _, err := conn.readFrame(); err == nil {
		t.Fatal("readFrame err = nil, want unmasked client frame error")
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
	payload, err := readServerTextFrame(siteZeroReader)
	if err != nil {
		t.Fatalf("read site zero initial payload: %v", err)
	}
	var site SiteStats
	if err := json.Unmarshal(payload, &site); err != nil {
		t.Fatalf("decode site zero initial payload: %v", err)
	}
	if site.SiteID != 0 {
		t.Fatalf("site id = %d, want 0", site.SiteID)
	}
	if site.Live != 1 {
		t.Fatalf("reserved site live = %d, want only site zero live users 1", site.Live)
	}

	siteOneConn, _, err := dialWebSocket(httpServer.URL, "/ws?site-id=1&visitor-id=customer")
	if err != nil {
		t.Fatalf("dial site one websocket: %v", err)
	}
	defer siteOneConn.Close()

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

func websocketHandshakeStatus(t *testing.T, serverURL string, path string, headers map[string]string) int {
	t.Helper()

	parsed, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	conn, err := net.Dial("tcp", parsed.Host)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	var request strings.Builder
	fmt.Fprintf(&request, "GET %s HTTP/1.1\r\n", path)
	fmt.Fprintf(&request, "Host: %s\r\n", parsed.Host)
	request.WriteString("Upgrade: websocket\r\n")
	request.WriteString("Connection: Upgrade\r\n")
	for key, value := range headers {
		fmt.Fprintf(&request, "%s: %s\r\n", key, value)
	}
	request.WriteString("\r\n")

	if _, err := conn.Write([]byte(request.String())); err != nil {
		t.Fatalf("write websocket handshake: %v", err)
	}

	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read websocket status: %v", err)
	}
	parts := strings.Fields(statusLine)
	if len(parts) < 2 {
		t.Fatalf("malformed websocket status line: %q", statusLine)
	}
	status, err := strconv.Atoi(parts[1])
	if err != nil {
		t.Fatalf("parse websocket status %q: %v", parts[1], err)
	}
	return status
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
