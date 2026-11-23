//go:build linux || darwin

package webui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type boundShortWriter struct{}

func (boundShortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestBoundReadRunPreflightAndShortOutput(t *testing.T) {
	if boundProcessChild(t) {
		return
	}
	f := newBoundFixture(t, true)
	t.Setenv("TRACK_WEB_DESKTOP_PARENT_PID", strconv.Itoa(os.Getppid()))
	t.Setenv("TRACK_WEB_DESKTOP_READY_TOKEN", strings.Repeat("a", 64))
	raw := boundProtocolWire(t, f, "preflight", "")
	before := boundInventory(t, f, true)
	for _, short := range []bool{false, true} {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := w.Write(raw); w.Close(); done <- err }()
		var output bytes.Buffer
		var writer io.Writer = &output
		if short {
			writer = boundShortWriter{}
		}
		err = RunBoundReadLaunch(context.Background(), r, writer, "", func() { t.Error("preflight emitted readiness") })
		if writeErr := <-done; writeErr != nil {
			t.Fatal(writeErr)
		}
		if short {
			if !errors.Is(err, io.ErrShortWrite) {
				t.Fatalf("short response accepted: %v", err)
			}
		} else if err != nil || !bytes.HasSuffix(output.Bytes(), []byte("\n")) {
			t.Fatalf("preflight response: %v", err)
		}
		assertBoundPipeClosed(t, r)
		boundUnchanged(t, f, before, true)
	}
}

func TestBoundReadRunRejectsBeforeConsumingInput(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		duplicate, err := syscall.Dup(int(r.Fd()))
		if err != nil {
			t.Fatal(err)
		}
		reader := os.NewFile(uintptr(duplicate), "retained-test-pipe")
		defer reader.Close()
		if _, err := w.WriteString("untouched input"); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		if canceled {
			cancel()
		} else {
			t.Setenv("TRACK_WEB_DESKTOP_PARENT_PID", "1")
		}
		var output bytes.Buffer
		err = RunBoundReadLaunch(ctx, r, &output, "", func() { t.Error("rejection emitted readiness") })
		cancel()
		w.Close()
		if !errors.Is(err, errBoundLaunchProtocol) || output.Len() != 0 {
			t.Fatalf("rejected launch published output: %v", err)
		}
		assertBoundPipeClosed(t, r)
		got, err := io.ReadAll(reader)
		if err != nil || string(got) != "untouched input" {
			t.Fatalf("input consumed before owner/cancellation check: %q, %v", got, err)
		}
	}
}
