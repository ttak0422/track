package webui

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestParseOGP(t *testing.T) {
	base, _ := url.Parse("https://example.com/articles/1")
	doc := `<html><head>
		<title>Fallback &amp; Title</title>
		<meta property="og:title" content="Real Title">
		<meta property="og:description" content="A &quot;great&quot; read">
		<meta property="og:image" content="/img/cover.png">
		<meta property="og:site_name" content="Example">
		<meta name="twitter:description" content="ignored because og wins">
	</head><body><meta property="og:title" content="too late"></body></html>`

	res := parseOGP(doc, base)
	if res.Title != "Real Title" {
		t.Fatalf("title: %q", res.Title)
	}
	if res.Description != `A "great" read` {
		t.Fatalf("description: %q", res.Description)
	}
	if res.Image != "https://example.com/img/cover.png" {
		t.Fatalf("image should resolve against the page: %q", res.Image)
	}
	if res.SiteName != "Example" {
		t.Fatalf("site_name: %q", res.SiteName)
	}
}

func TestParseOGPFallbacks(t *testing.T) {
	base, _ := url.Parse("https://blog.test/post")
	// No og:* tags: title comes from <title>, description from twitter, site_name from the host.
	doc := `<head><title>  Plain Title  </title>
		<meta name="twitter:description" content="tw desc">
		<meta name="twitter:image" content="https://cdn.test/x.jpg"></head>`
	res := parseOGP(doc, base)
	if res.Title != "Plain Title" {
		t.Fatalf("title fallback: %q", res.Title)
	}
	if res.Description != "tw desc" {
		t.Fatalf("twitter description fallback: %q", res.Description)
	}
	if res.Image != "https://cdn.test/x.jpg" {
		t.Fatalf("twitter image fallback: %q", res.Image)
	}
	if res.SiteName != "blog.test" {
		t.Fatalf("site_name should fall back to host: %q", res.SiteName)
	}
}

func TestParseOGPIgnoresBodyAndUnsafeImage(t *testing.T) {
	base, _ := url.Parse("https://example.com/")
	doc := `<head><meta property="og:title" content="Head Title">
		<meta property="og:image" content="javascript:alert(1)"></head>
		<body><meta property="og:title" content="Body Title"></body>`
	res := parseOGP(doc, base)
	if res.Title != "Head Title" {
		t.Fatalf("should only read head metadata: %q", res.Title)
	}
	if res.Image != "" {
		t.Fatalf("non-http image must be dropped, got %q", res.Image)
	}
}

func TestIsPublicIP(t *testing.T) {
	cases := map[string]bool{
		"8.8.8.8":          true,
		"1.1.1.1":          true,
		"127.0.0.1":        false,
		"10.0.0.5":         false,
		"192.168.1.10":     false,
		"172.16.4.4":       false,
		"169.254.1.1":      false,
		"0.0.0.0":          false,
		"::1":              false,
		"fc00::1":          false,
		"2606:4700:4700::": true,
	}
	for ipStr, want := range cases {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			t.Fatalf("bad test ip %q", ipStr)
		}
		if got := isPublicIP(ip); got != want {
			t.Errorf("isPublicIP(%s) = %v, want %v", ipStr, got, want)
		}
	}
}

func TestIsHTTPURL(t *testing.T) {
	cases := map[string]bool{
		"https://example.com":     true,
		"http://example.com/path": true,
		"ftp://example.com":       false,
		"javascript:alert(1)":     false,
		"/relative/path":          false,
		"example.com":             false,
	}
	for raw, want := range cases {
		if got := isHTTPURL(raw); got != want {
			t.Errorf("isHTTPURL(%q) = %v, want %v", raw, got, want)
		}
	}
}

// TestFetchOGPRefusesLoopback proves the SSRF guard: httptest binds to 127.0.0.1, which the dialer's
// Control hook must refuse so a note cannot reach loopback services through the OGP fetcher.
func TestFetchOGPRefusesLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<head><meta property="og:title" content="secret"></head>`))
	}))
	defer srv.Close()

	if _, err := fetchOGP(context.Background(), srv.URL); err == nil {
		t.Fatalf("expected loopback fetch to be refused by the SSRF guard")
	}
}
