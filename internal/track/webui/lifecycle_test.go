package webui

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/store"
)

func TestServeWithLifecycleAnnouncesOnlyBoundListenersAndStopsOnCancellation(t *testing.T) {
	cfg, st := lifecycleFixture(t)
	defer st.Close()
	mainListener, appsListener := reserveAdjacentListeners(t)
	addr := mainListener.Addr().String()
	appsAddr := appsListener.Addr().String()
	_ = mainListener.Close()
	_ = appsListener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- ServeWithLifecycle(ctx, cfg, st, addr, func() { ready <- struct{}{} })
	}()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not announce readiness after binding both ports")
	}

	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(2 * time.Second)
	for {
		response, err := client.Get("http://" + net.JoinHostPort("127.0.0.1", portOf(addr)) + "/api/vaults")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("workspace API did not become available after readiness: response=%v err=%v", response, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if conn, err := net.DialTimeout("tcp", appsAddr, time.Second); err != nil {
		t.Fatalf("static-app listener was not bound at readiness: %v", err)
	} else {
		conn.Close()
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ServeWithLifecycle after cancellation: %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("server did not stop after context cancellation")
	}
}

func TestServeWithLifecycleDoesNotAnnounceWhenStaticPortIsOccupied(t *testing.T) {
	cfg, st := lifecycleFixture(t)
	defer st.Close()
	mainListener, appsListener := reserveAdjacentListeners(t)
	addr := mainListener.Addr().String()
	_ = mainListener.Close()
	defer appsListener.Close()

	announced := false
	err := ServeWithLifecycle(context.Background(), cfg, st, addr, func() { announced = true })
	if err == nil || !strings.Contains(err.Error(), "static apps") {
		t.Fatalf("expected a static app bind failure, got %v", err)
	}
	if announced {
		t.Fatal("server announced readiness despite failing to bind its static-app port")
	}
}

func lifecycleFixture(t *testing.T) (*config.Config, *store.Store) {
	t.Helper()
	vault := t.TempDir()
	cache := t.TempDir()
	cfg := &config.Config{
		VaultDir:          vault,
		VaultDirDisplay:   vault,
		DBPath:            filepath.Join(cache, "index.db"),
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	return cfg, st
}

func reserveAdjacentListeners(t *testing.T) (net.Listener, net.Listener) {
	t.Helper()
	for range 20 {
		main, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		_, portString, err := net.SplitHostPort(main.Addr().String())
		if err != nil {
			main.Close()
			t.Fatal(err)
		}
		port, err := strconv.Atoi(portString)
		if err != nil || port >= 65535 {
			main.Close()
			continue
		}
		// Keep the test independent of privileged/busy neighboring ports by reserving the exact +1
		// address directly after parsing the main port.
		appsPort := port + 1
		apps, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(appsPort)))
		if err == nil {
			return main, apps
		}
		main.Close()
	}
	t.Fatal("could not reserve an adjacent test port pair")
	return nil, nil
}

func portOf(addr string) string {
	_, port, _ := net.SplitHostPort(addr)
	return port
}
