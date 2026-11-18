//go:build linux || darwin

package webui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
)

func TestManifestBindingCancellationAndStickyMismatch(t *testing.T) {
	f := newSentinelFixture(t)
	selector := config.LaunchSelector{Name: "work"}
	m, err := config.ResolveReadOnlyLaunchManifest(context.Background(), selector, f.cache)
	if err != nil {
		t.Fatal(err)
	}
	b, err := newManifestBoundBootstrap(context.Background(), selector, f.cache, m.Revision, strings.Repeat("a", 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	p := b.readPolicy
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(p.validateBinding(ctx), context.Canceled) {
		t.Fatal("canceled request not rejected")
	}
	if err = p.validateBinding(context.Background()); err != nil {
		t.Fatalf("cancellation poisoned binding: %v", err)
	}
	deadline, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	if !errors.Is(p.validateBinding(deadline), context.DeadlineExceeded) {
		t.Fatal("expired request not rejected")
	}
	if err = p.validateBinding(context.Background()); err != nil {
		t.Fatalf("deadline poisoned binding: %v", err)
	}
	if err = p.binding.checkConfig(context.Background(), f.cfg); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(f.configuration)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(f.configuration, append(original, []byte("\n# changed\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(p.validateBinding(context.Background()), config.ErrLaunchManifestChanged) {
		t.Fatal("drift accepted")
	}
	if err = os.WriteFile(f.configuration, original, 0600); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(p.validateBinding(context.Background()), config.ErrLaunchManifestChanged) {
		t.Fatal("restoration revived binding")
	}
	for _, path := range []string{"/api/capabilities", "/api/notes", "/api/storage"} {
		rr := httptest.NewRecorder()
		b.serveHTTP(rr, httptest.NewRequest("GET", path, nil))
		if rr.Code != 503 {
			t.Fatalf("%s: %d %s", path, rr.Code, rr.Body.String())
		}
	}
	if err = p.withConfig(f.cfg).validateBinding(context.Background()); !errors.Is(err, config.ErrLaunchManifestChanged) {
		t.Fatal("copy lost invalidation")
	}
}
func TestManifestBindingRejectsWrongConfigAndNonce(t *testing.T) {
	f := newSentinelFixture(t)
	m, err := config.ResolveReadOnlyLaunchManifest(context.Background(), config.LaunchSelector{Name: "main"}, f.cache)
	if err != nil {
		t.Fatal(err)
	}
	for _, nonce := range []string{"", strings.Repeat("x", 32), strings.Repeat("a", 130)} {
		if _, err = newManifestBoundBootstrap(context.Background(), m.Selector, f.cache, m.Revision, nonce, nil); err == nil {
			t.Fatal("bad nonce accepted")
		}
	}
	b, err := newManifestBoundBootstrap(context.Background(), m.Selector, f.cache, m.Revision, strings.Repeat("b", 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	altered := *f.cfg
	altered.DBPath = f.cfg.DBPath + ".other"
	if err = b.readPolicy.binding.checkConfig(context.Background(), &altered); !errors.Is(err, config.ErrLaunchManifestChanged) {
		t.Fatalf("wrong DB accepted: %v", err)
	}
}

func TestManifestBindingRejectsInFlightResponseAfterDrift(t *testing.T) {
	f := newSentinelFixture(t)
	m, err := config.ResolveReadOnlyLaunchManifest(context.Background(), config.LaunchSelector{Name: "main"}, f.cache)
	if err != nil {
		t.Fatal(err)
	}
	b, err := newManifestBoundBootstrap(context.Background(), m.Selector, f.cache, m.Revision, strings.Repeat("c", 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	admitted, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	response := httptest.NewRecorder()
	handler := b.readPolicy.boundResponse(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(admitted)
		<-release
		w.Write([]byte("stale accepted rows"))
	}))
	go func() { defer close(done); handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/notes", nil)) }()
	select {
	case <-admitted:
	case <-time.After(time.Second * 5):
		t.Fatal("handler not admitted")
	}
	raw, err := os.ReadFile(f.configuration)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(f.configuration, append(raw, []byte("\n# drift\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	failed := httptest.NewRecorder()
	b.readPolicy.capabilities(failed, httptest.NewRequest("GET", "/api/capabilities", nil))
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second * 5):
		t.Fatal("completion stalled")
	}
	if failed.Code != 503 || response.Code != 503 || strings.Contains(response.Body.String(), "stale accepted rows") {
		t.Fatalf("stale response escaped: %d %d %s", failed.Code, response.Code, response.Body.String())
	}
}

func TestManifestBindingFiniteResponseAndVersionIsolation(t *testing.T) {
	f := newSentinelFixture(t)
	m, err := config.ResolveReadOnlyLaunchManifest(context.Background(), config.LaunchSelector{Name: "main"}, f.cache)
	if err != nil {
		t.Fatal(err)
	}
	b, err := newManifestBoundBootstrap(context.Background(), m.Selector, f.cache, m.Revision, strings.Repeat("d", 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	called := false
	h := b.readPolicy.boundResponse(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write(make([]byte, responseByteLimit+1))
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/api/events", nil))
	if rr.Code != 403 || called {
		t.Fatal("bound stream admitted")
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/api/notes", nil))
	if rr.Code != 503 || !called {
		t.Fatal("oversize response admitted")
	}
	rr = httptest.NewRecorder()
	b.readPolicy.capabilities(rr, httptest.NewRequest("GET", "/api/capabilities", nil))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"version":2`) || !strings.Contains(rr.Body.String(), m.Revision) {
		t.Fatal(rr.Body.String())
	}
	var unbound *readPolicy
	old := httptest.NewRecorder()
	unbound.capabilities(old, httptest.NewRequest("GET", "/api/capabilities", nil))
	expected := httptest.NewRecorder()
	writeCapabilities(expected, httptest.NewRequest("GET", "/api/capabilities", nil), false)
	if old.Body.String() != expected.Body.String() {
		t.Fatal("v1 bytes changed")
	}
}

func TestManifestBindingLeaseMayReenterCapabilities(t *testing.T) {
	f := newSentinelFixture(t)
	m, err := config.ResolveReadOnlyLaunchManifest(context.Background(), config.LaunchSelector{Name: "main"}, f.cache)
	if err != nil {
		t.Fatal(err)
	}
	var b *Bootstrap
	lease := func(*config.Config) (func(), error) {
		rr := httptest.NewRecorder()
		b.readPolicy.capabilities(rr, httptest.NewRequest("GET", "/api/capabilities", nil))
		if rr.Code != 200 {
			return nil, errors.New("reentrant capability rejected")
		}
		return func() {}, nil
	}
	b, err = newManifestBoundBootstrap(context.Background(), m.Selector, f.cache, m.Revision, strings.Repeat("e", 32), lease)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	done := make(chan error, 1)
	go func() {
		release, err := b.lease(f.cfg)
		if release != nil {
			release()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reentrant lease deadlocked")
	}
}
