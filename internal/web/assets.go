package web

import (
	"fmt"
	"html"
)

func renderHome(stats NetworkStats) string {
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>SSPS - Stupid Simple Presence Service</title>
  <style>
    :root { color-scheme: light dark; font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    body { margin: 0; background: Canvas; color: CanvasText; }
    main { max-width: 880px; margin: 0 auto; padding: 48px 20px 64px; }
    h1 { font-size: 42px; line-height: 1.05; margin: 0 0 12px; }
    h2 { margin-top: 36px; }
    p, li { line-height: 1.6; }
    pre { overflow-x: auto; padding: 14px; border: 1px solid color-mix(in srgb, CanvasText 20%%, transparent); border-radius: 8px; }
    code { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
    .stats { display: grid; gap: 12px; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); margin: 28px 0; }
    .stat { border: 1px solid color-mix(in srgb, CanvasText 18%%, transparent); border-radius: 8px; padding: 14px; }
    .value { display: block; font-size: 28px; font-weight: 700; }
    .label { color: color-mix(in srgb, CanvasText 70%%, transparent); font-size: 14px; }
    .examples { display: grid; gap: 12px; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); margin: 16px 0 28px; }
    .example { border: 1px solid color-mix(in srgb, CanvasText 18%%, transparent); border-radius: 8px; padding: 14px; }
    .example [id^="ssps-"] { display: block; font-size: 28px; font-weight: 700; }
    a { color: LinkText; }
  </style>
</head>
<body>
  <main>
    <h1>Stupid Simple Presence Service</h1>
    <p>SSPS gives any website a tiny live visitor counter and visit counter with one script tag.</p>
    <p>The top stats are network-wide across every site using SSPS.</p>
    <div class="stats">
      <div class="stat"><span class="value" data-ssps-network-ids-created>%d</span><span class="label">IDs created</span></div>
      <div class="stat"><span class="value" data-ssps-network-live-users>%d</span><span class="label">users live now</span></div>
      <div class="stat"><span class="value" data-ssps-network-active-sites>%d</span><span class="label">active sites</span></div>
      <div class="stat"><span class="value" data-ssps-network-total-visits>%d</span><span class="label">total visits</span></div>
    </div>
    <h2>Get Started</h2>
    <p>Visit <a href="/generate">/generate</a> to create a numeric site ID and copy the script tag.</p>
    <pre><code>&lt;script async src="https://usessps.com/ssps.js" data-site-id="123"&gt;&lt;/script&gt;</code></pre>
    <h2>DOM Counters</h2>
    <p>The script updates these elements when they exist:</p>
    <pre><code>&lt;span id="ssps-live-count"&gt;&lt;/span&gt;
&lt;span id="ssps-visit-count"&gt;&lt;/span&gt;
&lt;span id="ssps-unique-visit-count"&gt;&lt;/span&gt;</code></pre>
    <p>This page uses the reserved SSPS site ID <code>0</code>, so these are live examples of those spans updating:</p>
    <div class="examples">
      <div class="example"><span id="ssps-live-count">0</span><span class="label">active visitors</span></div>
      <div class="example"><span id="ssps-visit-count">0</span><span class="label">visits to this page</span></div>
      <div class="example"><span id="ssps-unique-visit-count">0</span><span class="label">unique visitors here</span></div>
    </div>
    <h2>Programmatic API</h2>
    <pre><code>window.addEventListener("ssps:update", (event) =&gt; {
  console.log(event.detail.live, event.detail.totalHits, event.detail.uniqueVisitors)
})

const stats = window.SSPS.getStats()</code></pre>
    <h2>JSON API</h2>
    <ul>
      <li><code>GET /api/stats</code> for network totals.</li>
      <li><code>GET /api/sites/{siteID}/stats</code> for one site.</li>
    </ul>
  </main>
  <script async src="/ssps.js" data-site-id="0" data-network-stats></script>
</body>
</html>`, stats.IDsCreated, stats.LiveUsers, stats.ActiveSites, stats.TotalVisits)
}

func renderGenerate(siteID int64, scriptURL string) string {
	snippet := fmt.Sprintf(`<script async src="%s" data-site-id="%d"></script>`, html.EscapeString(scriptURL), siteID)
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>SSPS Site %d</title>
  <style>
    :root { color-scheme: light dark; font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    main { max-width: 760px; margin: 0 auto; padding: 48px 20px; }
    pre { overflow-x: auto; padding: 14px; border: 1px solid color-mix(in srgb, CanvasText 20%%, transparent); border-radius: 8px; }
    code { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
  </style>
</head>
<body>
  <main>
    <h1>Site ID %d</h1>
    <p>Embed this script on any page that should count toward this presence pool.</p>
    <pre><code>%s</code></pre>
  </main>
</body>
</html>`, siteID, siteID, html.EscapeString(snippet))
}

func scriptJS() string {
	return `(function () {
  var script = document.currentScript;
  var scriptURL = new URL(script && script.src ? script.src : "/ssps.js", window.location.href);
  var siteId = script && script.dataset ? script.dataset.siteId : "";
  if (!siteId) siteId = scriptURL.searchParams.get("site-id") || "";
  if (!siteId) return;

  var visitorKey = "ssps:visitor-id";
  var visitorId = "";
  try {
    visitorId = window.localStorage.getItem(visitorKey) || "";
    if (!visitorId) {
      visitorId = (window.crypto && crypto.randomUUID) ? crypto.randomUUID() : String(Date.now()) + "-" + Math.random().toString(16).slice(2);
      window.localStorage.setItem(visitorKey, visitorId);
    }
  } catch (_) {
    visitorId = String(Date.now()) + "-" + Math.random().toString(16).slice(2);
  }

  var state = { siteId: Number(siteId), live: 0, totalHits: 0, uniqueVisitors: 0 };
  var listeners = [];

  function setText(selector, value) {
    document.querySelectorAll(selector).forEach(function (element) {
      element.textContent = String(value);
    });
  }

  function publish(next) {
    state = next;
    setText("#ssps-live-count,[data-ssps-live-count]", state.live);
    setText("#ssps-visit-count,[data-ssps-visit-count]", state.totalHits);
    setText("#ssps-unique-visit-count,[data-ssps-unique-visit-count]", state.uniqueVisitors);
    listeners.forEach(function (listener) { listener(state); });
    window.dispatchEvent(new CustomEvent("ssps:update", { detail: state }));
    refreshNetworkStats();
  }

  function publishNetworkStats(stats) {
    setText("[data-ssps-network-ids-created]", stats.idsCreated);
    setText("[data-ssps-network-live-users]", stats.liveUsers);
    setText("[data-ssps-network-active-sites]", stats.activeSites);
    setText("[data-ssps-network-total-visits]", stats.totalVisits);
  }

  function refreshNetworkStats() {
    if (!script || !script.dataset || !("networkStats" in script.dataset) || !window.fetch) return;
    fetch(scriptURL.origin + "/api/stats", { cache: "no-store" })
      .then(function (response) { return response.ok ? response.json() : null; })
      .then(function (stats) { if (stats) publishNetworkStats(stats); })
      .catch(function () {});
  }

  window.SSPS = window.SSPS || {};
  window.SSPS.getStats = function () { return Object.assign({}, state); };
  window.SSPS.onUpdate = function (listener) {
    listeners.push(listener);
    listener(state);
    return function () {
      listeners = listeners.filter(function (candidate) { return candidate !== listener; });
    };
  };

  function connect() {
    var wsProtocol = scriptURL.protocol === "https:" ? "wss:" : "ws:";
    var wsURL = wsProtocol + "//" + scriptURL.host + "/ws?site-id=" + encodeURIComponent(siteId) + "&visitor-id=" + encodeURIComponent(visitorId);
    var socket = new WebSocket(wsURL);
    socket.onmessage = function (event) {
      try { publish(JSON.parse(event.data)); } catch (_) {}
    };
    socket.onclose = function () {
      window.setTimeout(connect, 3000);
    };
  }

  refreshNetworkStats();
  if (script && script.dataset && ("networkStats" in script.dataset)) {
    window.setInterval(refreshNetworkStats, 5000);
  }
  connect();
})();`
}
