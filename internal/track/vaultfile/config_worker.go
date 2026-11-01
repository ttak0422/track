package vaultfile

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
)

const configArg = "--track-internal-config-worker-v1"

var directReads bool
var configOnce sync.Once
var configPool atomic.Pointer[Pool]

// ServeConfigWorker is entered only by the owned config child after pure package
// initialization. Its loader reads config; it never opens a DB or executes a command.
func ServeConfigWorker(load func(string, bool) ([]byte, error)) {
	if len(os.Args) != 2 || os.Args[1] != configArg || os.Getenv("TRACK_INTERNAL_READ_WORKER") != "1" {
		return
	}
	directReads = true
	for {
		var r request
		if frameRead(os.Stdin, &r) != nil {
			os.Exit(0)
		}
		var v response
		if (r.Op != "config-read" && r.Op != "config-strict") || len(r.Path) > 8192 {
			v.Problem = "invalid configuration request"
		} else {
			raw, err := load(r.Path, r.Op == "config-strict")
			if err != nil {
				v.Problem = boundedMessage(err.Error())
			} else {
				v.Data = raw
			}
		}
		if frameWrite(os.Stdout, v) != nil {
			os.Exit(1)
		}
	}
}
func ConfigData(ctx context.Context, path string, strict bool) ([]byte, error) {
	configOnce.Do(func() {
		p := newPool(4, 4, func() (process, error) { return startMode(configArg) })
		p.ownerLimit = 1
		configPool.Store(p)
	})
	op := "config-read"
	if strict {
		op = "config-strict"
	}
	v, err := configPool.Load().call(ctx, request{Op: op, Path: path})
	if err != nil {
		return nil, err
	}
	return v.Data, v.inputError(path)
}
