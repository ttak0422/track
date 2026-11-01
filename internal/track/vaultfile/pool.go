package vaultfile

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const ReadTimeout = 2 * time.Second
const WorkerCount = 4
const QueueLimit = 32
const MaxReadBytes = 16 << 20
const maxFrame = 24 << 20

var ErrCapacity = errors.New("read worker queue is full")
var ErrClosed = errors.New("read workers are stopped")

type process interface {
	exchange(request) (response, error)
	stop()
	done() <-chan struct{}
}
type job struct {
	ctx     context.Context
	req     request
	answer  chan outcome
	release func()
}
type outcome struct {
	value response
	err   error
}

// Pool owns a fixed number of process slots, including children not yet reaped.
// A response deadline never waits for a blocked syscall or child termination.
type Pool struct {
	jobs              chan job
	closed            chan struct{}
	finished          chan struct{}
	once              sync.Once
	live              atomic.Int64
	quarantined       atomic.Int64
	launch            func() (process, error)
	ownerMu           sync.Mutex
	owners            map[string]int
	ownerLimit        int
	admission         chan struct{}
	ownerChanged      chan struct{}
	quarantinedOwners map[string]int
	tickets           map[string]int
	waitLimit         int
}

func NewPool() *Pool { return newPool(WorkerCount, QueueLimit, startProcess) }
func newPool(n, queue int, launch func() (process, error)) *Pool {
	p := &Pool{jobs: make(chan job, queue), closed: make(chan struct{}), finished: make(chan struct{}), launch: launch, owners: map[string]int{}, ownerLimit: 2, admission: make(chan struct{}, n+queue), ownerChanged: make(chan struct{}), quarantinedOwners: map[string]int{}, tickets: map[string]int{}, waitLimit: max(1, queue/n)}
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() { defer wg.Done(); p.loop() }()
	}
	go func() { wg.Wait(); close(p.finished) }()
	return p
}
func (p *Pool) call(ctx context.Context, r request) (response, error) {
	if len(r.Path) > 8192 || len(r.Meta) > 8192 {
		return response{}, errors.New("read path exceeds budget")
	}
	owner := ownerFor(r.Path)
	p.ownerMu.Lock()
	if p.tickets[owner] >= p.ownerLimit+p.waitLimit {
		p.ownerMu.Unlock()
		return response{}, ErrCapacity
	}
	p.tickets[owner]++
	p.ownerMu.Unlock()
	defer func() {
		p.ownerMu.Lock()
		p.tickets[owner]--
		if p.tickets[owner] == 0 {
			delete(p.tickets, owner)
		}
		p.ownerMu.Unlock()
	}()
	select {
	case p.admission <- struct{}{}:
		defer func() { <-p.admission }()
	default:
		return response{}, ErrCapacity
	}
	for {
		p.ownerMu.Lock()
		if p.owners[owner] < p.ownerLimit {
			p.owners[owner]++
			p.ownerMu.Unlock()
			break
		}
		// Quarantined slots cannot promise progress. Ordinary short concurrent
		// reads instead wait on a bounded admission ticket, without polling.
		if p.quarantinedOwners[owner] > 0 {
			p.ownerMu.Unlock()
			return response{}, ErrCapacity
		}
		changed := p.ownerChanged
		p.ownerMu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return response{}, ctx.Err()
		case <-p.closed:
			return response{}, ErrClosed
		}
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			p.ownerMu.Lock()
			p.owners[owner]--
			if p.owners[owner] == 0 {
				delete(p.owners, owner)
			}
			close(p.ownerChanged)
			p.ownerChanged = make(chan struct{})
			p.ownerMu.Unlock()
		})
	}
	j := job{ctx: ctx, req: r, answer: make(chan outcome, 1), release: release}
	select {
	case <-p.closed:
		release()
		return response{}, ErrClosed
	default:
	}
	select {
	case p.jobs <- j:
	case <-ctx.Done():
		release()
		return response{}, ctx.Err()
	default:
		release()
		return response{}, ErrCapacity
	}
	select {
	case out := <-j.answer:
		return out.value, out.err
	case <-ctx.Done():
		return response{}, ctx.Err()
	case <-p.closed:
		return response{}, ErrClosed
	}
}
func (p *Pool) loop() {
	var child process
	retire := func(owner string) {
		if child == nil {
			return
		}
		p.quarantined.Add(1)
		if owner != "" {
			p.ownerMu.Lock()
			p.quarantinedOwners[owner]++
			close(p.ownerChanged)
			p.ownerChanged = make(chan struct{})
			p.ownerMu.Unlock()
		}
		child.stop()
		<-child.done()
		if owner != "" {
			p.ownerMu.Lock()
			p.quarantinedOwners[owner]--
			if p.quarantinedOwners[owner] == 0 {
				delete(p.quarantinedOwners, owner)
			}
			p.ownerMu.Unlock()
		}
		p.quarantined.Add(-1)
		p.live.Add(-1)
		child = nil
	}
	defer retire("")
	for {
		select {
		case <-p.closed:
			return
		case j := <-p.jobs:
			if j.ctx.Err() != nil {
				j.release()
				continue
			}
			if child == nil {
				// Starting a child also consumes its slot if the OS has not returned yet.
				p.live.Add(1)
				var err error
				child, err = p.launch()
				if err != nil {
					p.live.Add(-1)
					j.answer <- outcome{err: err}
					j.release()
					continue
				}
			}
			answer := make(chan outcome, 1)
			current := child
			go func() { v, err := current.exchange(j.req); answer <- outcome{v, err} }()
			select {
			case out := <-answer:
				if out.err != nil {
					retire(ownerFor(j.req.Path))
				}
				if j.ctx.Err() == nil {
					j.release()
					j.answer <- out
				}
			case <-j.ctx.Done():
				retire(ownerFor(j.req.Path))
			case <-p.closed:
				retire(ownerFor(j.req.Path))
				j.release()
				return
			}
			j.release()
		}
	}
}
func (p *Pool) Close(ctx context.Context) error {
	p.once.Do(func() { close(p.closed) })
	select {
	case <-p.finished:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type WorkerStats struct {
	Owned, Quarantined int64
	Queued             int
}

func (p *Pool) Stats() WorkerStats {
	return WorkerStats{p.live.Load(), p.quarantined.Load(), len(p.jobs)}
}

var defaultOnce sync.Once
var defaultPool atomic.Pointer[Pool]

func workers() *Pool {
	defaultOnce.Do(func() { defaultPool.Store(NewPool()) })
	return defaultPool.Load()
}
func CloseWorkers(ctx context.Context) error {
	var first error
	for _, p := range []*Pool{defaultPool.Load(), configPool.Load()} {
		if p != nil {
			if err := p.Close(ctx); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

func bounded(r request) (response, error) {
	if directReads {
		v := perform(r)
		if r.Op != "read" && r.Op != "note" && v.Errno != 0 {
			return v, &os.PathError{Op: r.Op, Path: r.Path, Err: syscall.Errno(v.Errno)}
		}
		return v, v.inputError(r.Path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), ReadTimeout)
	defer cancel()
	v, err := workers().call(ctx, r)
	if err != nil {
		return response{}, &Error{Path: r.Path, Reason: "read unavailable: " + err.Error(), Err: err}
	}
	if r.Op != "read" && r.Op != "note" && v.Errno != 0 {
		return v, &os.PathError{Op: r.Op, Path: r.Path, Err: syscall.Errno(v.Errno)}
	}
	return v, v.inputError(r.Path)
}
