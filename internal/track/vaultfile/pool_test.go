package vaultfile

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"github.com/ttak0422/track/internal/track/notedata"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type controlledProcess struct {
	run     func(request) (response, error)
	stopped chan struct{}
	exited  chan struct{}
	once    sync.Once
}

func (p *controlledProcess) exchange(r request) (response, error) { return p.run(r) }
func (p *controlledProcess) stop()                                { p.once.Do(func() { close(p.stopped) }) }
func (p *controlledProcess) done() <-chan struct{}                { return p.exited }
func TestQuarantinedWorkersRetainBudgetAndHealthyPeerContinues(t *testing.T) {
	var launched atomic.Int32
	started := make(chan *controlledProcess, 4)
	release := make(chan struct{})
	p := newPool(4, 4, func() (process, error) {
		launched.Add(1)
		child := &controlledProcess{stopped: make(chan struct{}), exited: make(chan struct{})}
		child.run = func(r request) (response, error) {
			started <- child
			if r.Path == "/healthy/note/1.md" {
				return response{Data: []byte("verified")}, nil
			}
			<-release
			return response{}, context.Canceled
		}
		return child, nil
	})
	defer func() {
		close(release)
		for {
			select {
			case child := <-started:
				select {
				case <-child.exited:
				default:
					close(child.exited)
				}
			default:
				return
			}
		}
	}()
	quarantined := []*controlledProcess{}
	for range 2 {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { _, err := p.call(ctx, request{Op: "read", Path: "/bad/note/1.md"}); done <- err }()
		child := <-started
		quarantined = append(quarantined, child)
		cancel()
		if !errors.Is(<-done, context.Canceled) {
			t.Fatal("cancel did not return independently")
		}
		<-child.stopped
	}
	for range 20 {
		ctx, cancel := context.WithCancel(context.Background())
		_, err := p.call(ctx, request{Op: "read", Path: "/bad/note/2.md"})
		cancel()
		if !errors.Is(err, ErrCapacity) {
			t.Fatalf("bad vault obtained a replacement: %v", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := p.call(ctx, request{Op: "read", Path: "/healthy/note/1.md"})
	healthy := <-started
	if err != nil || string(result.Data) != "verified" || launched.Load() != 3 {
		t.Fatalf("healthy peer blocked or fork budget lost: %q %v %d", result.Data, err, launched.Load())
	}
	closeCtx, stop := context.WithCancel(context.Background())
	stop()
	if p.Close(closeCtx) == nil {
		t.Fatal("unreaped children reported stopped")
	}
	if got := p.Stats(); got.Owned < 2 || got.Quarantined < 2 {
		t.Fatalf("unreaped capacity forgotten: %+v", got)
	}
	for _, child := range append(quarantined, healthy) {
		select {
		case <-child.exited:
		default:
			close(child.exited)
		}
	}
	finish, finishCancel := context.WithTimeout(context.Background(), time.Second)
	defer finishCancel()
	if err := p.Close(finish); err != nil {
		t.Fatal(err)
	}
}
func TestQueueAndSpawnArePartOfDeadline(t *testing.T) {
	spawn := make(chan struct{})
	entered := make(chan struct{})
	p := newPool(1, 1, func() (process, error) { close(entered); <-spawn; return nil, errors.New("controlled spawn failure") })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := p.call(ctx, request{Path: "/first"}); done <- err }()
	<-entered
	cancel()
	if !errors.Is(<-done, context.Canceled) {
		t.Fatal("spawn blocked response")
	}
	second, stop := context.WithCancel(context.Background())
	queued := make(chan error, 1)
	go func() { _, err := p.call(second, request{Path: "/second"}); queued <- err }()
	// Queue observation is bounded; no process or filesystem polling is used.
	deadline := time.After(time.Second)
	for p.Stats().Queued == 0 {
		select {
		case <-deadline:
			t.Fatal("request not queued")
		default:
			runtime.Gosched()
		}
	}
	third, thirdCancel := context.WithTimeout(context.Background(), time.Second)
	defer thirdCancel()
	if _, err := p.call(third, request{Path: "/third"}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("queue grew: %v", err)
	}
	stop()
	if !errors.Is(<-queued, context.Canceled) {
		t.Fatal("queued deadline ignored")
	}
	close(spawn)
	finish, finishCancel := context.WithTimeout(context.Background(), time.Second)
	defer finishCancel()
	if err := p.Close(finish); err != nil {
		t.Fatal(err)
	}
}
func TestOwnedWorkerProtocolDoesNotRunCLIOrCreateMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "1.md")
	meta := filepath.Join(dir, "missing.yaml")
	if err := os.WriteFile(path, []byte("Readable body"), 0600); err != nil {
		t.Fatal(err)
	}
	p := NewPool()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	v, err := p.call(ctx, request{Op: "note", Path: path, Meta: meta})
	if err != nil || v.Problem != "" || v.Note == nil || v.Note.Body != "Readable body" {
		t.Fatalf("worker parse: %+v %v", v, err)
	}
	if _, err := os.Stat(meta); !os.IsNotExist(err) {
		t.Fatal("read worker created metadata", err)
	}
	if err := p.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if got := p.Stats(); got.Owned != 0 {
		t.Fatalf("owned process not reaped: %+v", got)
	}
}

func TestWorkerPreservesMetadataTypesAndExtensionsWithoutAliases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "1.md")
	metaPath := filepath.Join(dir, "1.yaml")
	original := []byte("version: 12\ntitle: Typed\nprops:\n  large: 18446744073709551615\n  day: 2024-01-02\nopaque: &fields\n  keep: true\n  nested: [1, null, text]\ncopy: *fields\n")
	if err := os.WriteFile(path, []byte("body"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metaPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	p := NewPool()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	defer p.Close(ctx)
	result, err := p.call(ctx, request{Op: "note", Path: path, Meta: metaPath})
	if err != nil || result.Problem != "" || result.Note == nil {
		t.Fatalf("parse: %+v %v", result, err)
	}
	var before, after notedata.Metadata
	if err := yaml.Unmarshal(original, &before); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(result.Note.Metadata, &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("metadata values changed: %#v %#v", before, after)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(result.Note.Metadata, &root); err != nil {
		t.Fatal(err)
	}
	var check func(*yaml.Node)
	check = func(n *yaml.Node) {
		if n.Kind == yaml.AliasNode {
			t.Fatal("parent received untrusted alias graph")
		}
		for _, child := range n.Content {
			check(child)
		}
	}
	check(&root)
	unchanged, err := os.ReadFile(metaPath)
	if err != nil || !bytes.Equal(original, unchanged) {
		t.Fatal("read worker rewrote sidecar", err)
	}
}
func TestWorkerRejectsOverBudgetMetadataStructureAndProtocol(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "1.md")
	meta := filepath.Join(dir, "1.yaml")
	os.WriteFile(path, []byte("body"), 0600)
	os.WriteFile(meta, []byte("version: 1\nopaque: "+strings.Repeat("[", 70)+"x"+strings.Repeat("]", 70)+"\n"), 0600)
	result := perform(request{Op: "note", Path: path, Meta: meta})
	if !result.Invalid || !strings.Contains(result.Problem, "budget") {
		t.Fatalf("deep metadata admitted: %+v", result)
	}
	var frame bytes.Buffer
	binary.Write(&frame, binary.LittleEndian, uint32(maxFrame+1))
	var decoded response
	if frameRead(&frame, &decoded) == nil {
		t.Fatal("oversized protocol frame admitted")
	}
}
