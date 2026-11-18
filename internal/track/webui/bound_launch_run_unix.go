//go:build linux || darwin

package webui

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/ttak0422/track/internal/track/config"
)

// RunBoundReadLaunch consumes one supervised child's bounded stdin request.
// Preflight only observes configuration; serve uses the exact bound revision.
// The caller must cancel ctx on parent death/signals and drain process output.
// This transport grants no native UI admission or canonical editing permission.
func RunBoundReadLaunch(ctx context.Context, input *os.File, output io.Writer, addr string, onReady func()) error {
	if input == nil {
		return errBoundLaunchProtocol
	}
	defer input.Close()
	if ctx == nil || ctx.Err() != nil || checkBoundLaunchOwner() != nil {
		return errBoundLaunchProtocol
	}
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	raw, err := readBoundLaunchPipe(readCtx, input)
	cancel()
	if err != nil {
		return errBoundLaunchProtocol
	}
	request, err := parseBoundLaunch(raw)
	if err != nil {
		return errBoundLaunchProtocol
	}
	if *request.Mode == "preflight" {
		if addr != "" || output == nil {
			return errBoundLaunchProtocol
		}
		result, err := dispatchBoundLaunch(ctx, raw, nil)
		if err != nil {
			return errBoundLaunchProtocol
		}
		encoded, err := json.Marshal(result.Manifest)
		if err != nil || len(encoded)+1 > 128<<10 || ctx.Err() != nil {
			return errBoundLaunchProtocol
		}
		encoded = append(encoded, '\n')
		n, err := output.Write(encoded)
		if err == nil && n != len(encoded) {
			return io.ErrShortWrite
		}
		return err
	}
	host, port, err := net.SplitHostPort(addr)
	number, portErr := strconv.Atoi(port)
	if err != nil || host != "127.0.0.1" || portErr != nil || number < 1024 || number >= 65535 || strconv.Itoa(number) != port {
		return errBoundLaunchProtocol
	}
	result, err := dispatchBoundLaunch(ctx, raw, func(cfg *config.Config) (func(), error) {
		return AcquireWebPID(cfg, addr)
	})
	if err != nil {
		return errBoundLaunchProtocol
	}
	defer result.Bootstrap.Close()
	if ctx.Err() != nil {
		return errBoundLaunchProtocol
	}
	return ServeBootstrapWithLifecycle(ctx, result.Bootstrap, addr, func() {
		if ctx.Err() == nil && onReady != nil {
			onReady()
		}
	})
}
