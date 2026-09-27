package webui

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func writeStaticTestApp(t *testing.T, vault, name, body string) {
	t.Helper()
	dir := filepath.Join(vault, "apps", name)
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log('"+name+"')"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.css"), []byte("body { color: red; }"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func startStaticAppsTestServer(t *testing.T, srv *Server) string {
	t.Helper()
	server := httptest.NewServer(srv.staticAppsHandler())
	t.Cleanup(server.Close)
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	srv.appsPort = port
	srv.appsBindHost = "127.0.0.1"
	return server.URL
}

func TestStaticAppsAddressUsesAdjacentPortAndCanonicalHost(t *testing.T) {
	tests := []struct {
		addr     string
		port     int
		wantAddr string
		wantHost string
		wantPort int
	}{
		{addr: "127.0.0.1:8765", port: 8765, wantAddr: "127.0.0.1:8766", wantHost: "127.0.0.1", wantPort: 8766},
		{addr: "localhost:8765", port: 8765, wantAddr: "localhost:8766", wantHost: "localhost", wantPort: 8766},
		{addr: "[::1]:8765", port: 8765, wantAddr: "[::1]:8766", wantHost: "::1", wantPort: 8766},
		{addr: "0.0.0.0:8765", port: 8765, wantAddr: "127.0.0.1:8766", wantHost: "127.0.0.1", wantPort: 8766},
		{addr: "[::]:8765", port: 8765, wantAddr: "127.0.0.1:8766", wantHost: "127.0.0.1", wantPort: 8766},
		{addr: ":8765", port: 8765, wantAddr: "127.0.0.1:8766", wantHost: "127.0.0.1", wantPort: 8766},
		{addr: "192.0.2.20:8765", port: 8765, wantAddr: "192.0.2.20:8766", wantHost: "192.0.2.20", wantPort: 8766},
		{addr: "track.example:8765", port: 8765, wantAddr: "track.example:8766", wantHost: "track.example", wantPort: 8766},
		{addr: "127.0.0.1:0", port: 43120, wantAddr: "127.0.0.1:43121", wantHost: "127.0.0.1", wantPort: 43121},
	}
	for _, tc := range tests {
		t.Run(tc.addr, func(t *testing.T) {
			addr, host, port, err := staticAppsAddress(tc.addr, tc.port)
			if err != nil {
				t.Fatal(err)
			}
			if addr != tc.wantAddr || host != tc.wantHost || port != tc.wantPort {
				t.Fatalf("staticAppsAddress(%q,%d) = (%q,%q,%d), want (%q,%q,%d)", tc.addr, tc.port, addr, host, port, tc.wantAddr, tc.wantHost, tc.wantPort)
			}
		})
	}
	for _, port := range []int{0, 65535, 65536} {
		if _, _, _, err := staticAppsAddress("127.0.0.1:8765", port); err == nil {
			t.Errorf("staticAppsAddress with bound workspace port %d succeeded", port)
		}
	}
}

func TestListenStaticAppsFailsOnAdjacentPortCollision(t *testing.T) {
	for attempt := 0; attempt < 20; attempt++ {
		workspace, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		workspacePort := workspace.Addr().(*net.TCPAddr).Port
		if workspacePort >= 65535 {
			workspace.Close()
			continue
		}
		collision, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(workspacePort+1)))
		if err != nil {
			workspace.Close()
			continue
		}
		_, _, _, err = listenStaticApps("127.0.0.1:0", workspace)
		collision.Close()
		workspace.Close()
		if err == nil || !strings.Contains(err.Error(), "workspace port + 1") {
			t.Fatalf("adjacent port collision error = %v, want clear no-fallback error", err)
		}
		return
	}
	t.Skip("could not reserve an adjacent loopback port for the collision test")
}

func TestAppRedirectUsesCanonicalHostnameAndUnitFallback(t *testing.T) {
	srv, workspace, main, _ := twoVaultWorkspace(t)
	writeStaticTestApp(t, main, "demo", "<h1>demo</h1>")
	srv.appsPort = "8766"
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	for _, tc := range []struct {
		requestHost string
		bindHost    string
		wantHost    string
	}{
		{requestHost: "localhost", bindHost: "127.0.0.1", wantHost: "127.0.0.1:8766"},
		{requestHost: "127.0.0.1", bindHost: "localhost", wantHost: "localhost:8766"},
		{requestHost: "localhost", bindHost: "", wantHost: "localhost:8766"},
	} {
		srv.appsBindHost = tc.bindHost
		req, err := http.NewRequest(http.MethodGet, workspace.URL+"/apps/demo/", nil)
		if err != nil {
			t.Fatal(err)
		}
		_, port, _ := net.SplitHostPort(req.URL.Host)
		req.Host = net.JoinHostPort(tc.requestHost, port)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		location, err := url.Parse(resp.Header.Get("Location"))
		if err != nil {
			t.Fatalf("redirect Location %q: %v", resp.Header.Get("Location"), err)
		}
		if resp.StatusCode != http.StatusFound || location.Host != tc.wantHost {
			t.Errorf("request host %q, bind host %q: redirect status=%d host=%q, want %q", tc.requestHost, tc.bindHost, resp.StatusCode, location.Host, tc.wantHost)
		}
	}
}

func TestStaticAppLaunchPreservesVaultAndQuery(t *testing.T) {
	srv, workspace, main, work := twoVaultWorkspace(t)
	writeStaticTestApp(t, main, "counter", "<h1>main</h1>")
	writeStaticTestApp(t, work, "counter", "<h1>work</h1>")
	appsURL := startStaticAppsTestServer(t, srv)

	resp, err := workspace.Client().Get(workspace.URL + "/apps/counter?__track_vault=work&vault=app-value&q=a%2Fb")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "work") || strings.Contains(string(body), "main") {
		t.Fatalf("launch should open the selected vault's real top-level HTML: status=%d body=%q", resp.StatusCode, body)
	}
	if contentType := resp.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("app index Content-Type = %q", contentType)
	}
	if got := resp.Request.URL.Query(); got.Get("vault") != "app-value" || got.Get("q") != "a/b" || got.Has(appVaultQuery) {
		t.Fatalf("app query should be preserved and the internal vault selector removed, got %v", got)
	}
	workspaceURL, _ := url.Parse(workspace.URL)
	if resp.Request.URL.Host == "" || resp.Request.URL.Port() == "" || resp.Request.URL.Host == workspaceURL.Host {
		t.Fatalf("app should be served from a separate origin, workspace=%s app=%s", workspace.URL, resp.Request.URL)
	}
	if _, opened := srv.cachedView("work"); opened {
		t.Fatal("static app launch should not open the vault's index/store")
	}
	if !strings.HasPrefix(resp.Request.URL.String(), appsURL+"/") {
		t.Fatalf("app came from unexpected listener: %s", resp.Request.URL)
	}

	assetURL := resp.Request.URL.ResolveReference(&url.URL{Path: "assets/app.js"})
	assetResp, err := workspace.Client().Get(assetURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer assetResp.Body.Close()
	assetBody, err := io.ReadAll(assetResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if assetResp.StatusCode != http.StatusOK || string(assetBody) != "console.log('counter')" {
		t.Fatalf("relative app dependency: status=%d body=%q", assetResp.StatusCode, assetBody)
	}
	if contentType := assetResp.Header.Get("Content-Type"); contentType != "text/javascript; charset=utf-8" {
		t.Fatalf("JavaScript Content-Type = %q", contentType)
	}
}

func TestStaticAppsHaveNoListingFallbackOrForeignHost(t *testing.T) {
	srv, _, main, _ := twoVaultWorkspace(t)
	writeStaticTestApp(t, main, "demo", "<h1>demo</h1>")
	appsURL := startStaticAppsTestServer(t, srv)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	for _, path := range []string{
		"/__track/apps/launch/demo/assets/",
		"/__track/apps/launch/demo/missing-route",
		"/__track/apps/launch/demo/../../secret.txt",
		"/__track/apps/launch/demo/%2e%2e/%2e%2e/secret.txt",
		"/api/search?q=secret",
		"/notes/100",
	} {
		resp, err := client.Get(appsURL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, resp.StatusCode)
		}
	}
	resp, err := client.Get(appsURL + "/__track/apps/launch/demo")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusPermanentRedirect || resp.Header.Get("Location") != "/__track/apps/launch/demo/" {
		t.Fatalf("app origin missing-slash redirect = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	request, err := http.NewRequest(http.MethodGet, appsURL+"/__track/apps/launch/demo/", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "evil.example:" + srv.appsPort
	resp, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign Host status = %d, want 403", resp.StatusCode)
	}
}

func TestStaticAppCannotReadWorkspaceAPI(t *testing.T) {
	srv, workspace, main, _ := twoVaultWorkspace(t)
	writeStaticTestApp(t, main, "demo", "<h1>demo</h1>")
	appsURL := startStaticAppsTestServer(t, srv)

	for _, tc := range []struct {
		method string
		path   string
		ws     bool
	}{
		{method: http.MethodGet, path: "/api/search?q=secret"},
		{method: http.MethodPost, path: "/api/note?id=100"},
		{method: http.MethodGet, path: "/api/events", ws: true},
	} {
		req, err := http.NewRequest(tc.method, workspace.URL+tc.path, strings.NewReader(`{"body":"changed"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", appsURL)
		if tc.ws {
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Upgrade", "websocket")
		}
		resp, err := workspace.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s from app origin = %d, want 403", tc.method, tc.path, resp.StatusCode)
		}
	}
	for _, header := range []struct{ name, value string }{
		{name: "Sec-Fetch-Site", value: "same-site"},
		{name: "Referer", value: appsURL + "/__track/apps/launch/demo/"},
	} {
		req, err := http.NewRequest(http.MethodGet, workspace.URL+"/api/search?q=secret", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(header.name, header.value)
		resp, err := workspace.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("GET /api/search with %s and no Origin = %d, want 403", header.name, resp.StatusCode)
		}
	}
}

func TestServeListenersClosesAppListenerWithWorkspace(t *testing.T) {
	srv, _, _, _ := twoVaultWorkspace(t)
	var mainListener, appListener net.Listener
	var appHost, appPort string
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		mainListener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("bind workspace listener: %v", err)
		}
		appListener, appHost, appPort, err = listenStaticApps("127.0.0.1:0", mainListener)
		if err == nil {
			break
		}
		mainListener.Close()
		mainListener = nil
	}
	if mainListener == nil || appListener == nil {
		t.Fatalf("could not bind adjacent listeners: %v", err)
	}
	mainPort := mainListener.Addr().(*net.TCPAddr).Port
	if got := appListener.Addr().(*net.TCPAddr).Port; got != mainPort+1 {
		t.Fatalf("app listener port = %d, want adjacent port %d", got, mainPort+1)
	}
	srv.bindHost = "127.0.0.1"
	srv.appsBindHost = appHost
	srv.appsPort = appPort
	done := make(chan error, 1)
	go func() { done <- serveListeners(mainListener, appListener, srv) }()

	deadline := time.Now().Add(time.Second)
	for {
		conn, err := net.DialTimeout("tcp", appListener.Addr().String(), 20*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("app listener did not start: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := mainListener.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("serveListeners did not return after the workspace listener stopped")
	}
	if conn, err := net.DialTimeout("tcp", appListener.Addr().String(), 50*time.Millisecond); err == nil {
		conn.Close()
		t.Fatal("static app listener remained open after workspace shutdown")
	}
}
