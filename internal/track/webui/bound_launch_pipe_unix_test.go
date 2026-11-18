//go:build linux || darwin

package webui

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func assertBoundPipeClosed(t *testing.T, file *os.File) {
	t.Helper()
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("owned descriptor not closed: %v", err)
	}
}
func TestBoundLaunchPipePayloadAndEOF(t *testing.T) {
	for _, payload := range [][]byte{nil, []byte(`{"partial":`), []byte("exact wire\n")} {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(payload); err != nil {
			t.Fatal(err)
		}
		w.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		got, err := readBoundLaunchPipe(ctx, r)
		cancel()
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("transport changed bytes: %v", err)
		}
		assertBoundPipeClosed(t, r)
		// Transport does not interpret or repair partial JSON.
		if len(payload) > 0 {
			if _, err := parseBoundLaunch(got); err == nil {
				t.Fatal("invalid request accepted")
			}
		}
	}
}
func TestBoundLaunchPipeBounds(t *testing.T) {
	for _, size := range []int{256 << 10, (256 << 10) + 1} {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		payload := bytes.Repeat([]byte{'x'}, size)
		// The only goroutine is the test writer; completion is always joined.
		done := make(chan struct{})
		go func() { defer close(done); defer w.Close(); _, _ = w.Write(payload) }()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		got, err := readBoundLaunchPipe(ctx, r)
		cancel()
		w.Close()
		<-done
		assertBoundPipeClosed(t, r)
		if size == 256<<10 {
			if err != nil || !bytes.Equal(got, payload) {
				t.Fatal("exact bound rejected", err)
			}
		} else if !errors.Is(err, errBoundLaunchPipe) || got != nil {
			t.Fatal("oversize accepted")
		}
	}
}
func TestBoundLaunchPipeRejectsHeldOpenCancelledAndWrongSources(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte("partial")); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		if cancelled {
			cancel()
		}
		start := time.Now()
		got, err := readBoundLaunchPipe(ctx, r)
		cancel()
		w.Close()
		if !errors.Is(err, errBoundLaunchPipe) || got != nil {
			t.Fatal("unterminated pipe accepted")
		}
		if time.Since(start) > time.Second {
			t.Fatal("read ignored cancellation deadline")
		}
		assertBoundPipeClosed(t, r)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err = readBoundLaunchPipe(context.Background(), r); !errors.Is(err, errBoundLaunchPipe) {
		t.Fatal("unbounded context accepted")
	}
	assertBoundPipeClosed(t, r)
	file, err := os.CreateTemp(t.TempDir(), "regular")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err = readBoundLaunchPipe(ctx, file); !errors.Is(err, errBoundLaunchPipe) {
		t.Fatal("regular file accepted")
	}
	assertBoundPipeClosed(t, file)
}

func TestBoundLaunchPipeCancellationDuringHeldOpenRead(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan struct{})
	timer := time.AfterFunc(20*time.Millisecond, func() { cancel(); close(done) })
	start := time.Now()
	got, err := readBoundLaunchPipe(ctx, r)
	if !timer.Stop() {
		<-done
	}
	if !errors.Is(err, errBoundLaunchPipe) || got != nil {
		t.Fatal("canceled read accepted")
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("in-flight cancellation did not interrupt read")
	}
	assertBoundPipeClosed(t, r)
}
