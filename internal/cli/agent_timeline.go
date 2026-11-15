package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/timeline"
)

// cmdTimelineProject is the explicit agent-record transport. Markdown checkbox
// snapshots continue to use `track timeline`; these IDs are durable project IDs.
func cmdTimelineProject(args []string) int {
	if len(args) == 0 {
		return fail("usage: track timeline-project list|get|apply [--id ID]")
	}
	action := args[0]
	if action != "list" && action != "get" && action != "apply" {
		return fail("unknown timeline-project action %q", action)
	}
	fs := flag.NewFlagSet("timeline-project "+action, flag.ContinueOnError)
	id := fs.String("id", "", "stable timeline project id")
	if code, ok := parseArgs(fs, args[1:]); !ok {
		return code
	}
	if fs.NArg() != 0 {
		return fail("unexpected positional arguments")
	}
	if action != "list" && strings.TrimSpace(*id) == "" {
		return fail("--id is required")
	}
	if action == "list" && *id != "" {
		return fail("list does not accept --id")
	}
	var request timeline.Request
	if action == "apply" {
		raw, err := io.ReadAll(io.LimitReader(os.Stdin, (1<<20)+1))
		if err != nil {
			return fail("read request: %v", err)
		}
		if len(raw) > 1<<20 {
			return fail("timeline request exceeds 1 MiB")
		}
		if err := decodeTimelineRequest(raw, &request); err != nil {
			return fail("decode request: %v", err)
		}
	}
	cfg, index, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer index.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store := timeline.NewStore(cfg.VaultDir)
	switch action {
	case "list":
		projects, err := store.List(ctx)
		if err != nil {
			return fail("timeline projects: %v", err)
		}
		if projects == nil {
			projects = []timeline.Summary{}
		}
		return emit(map[string]any{"projects": projects})
	case "get":
		snapshot, err := store.Read(ctx, *id)
		if err != nil {
			return fail("read timeline project: %v", err)
		}
		return emit(snapshot)
	default:
		snapshot, err := store.Apply(ctx, *id, request)
		if err != nil {
			return fail("apply timeline event: %v", err)
		}
		return emit(snapshot)
	}
}

func decodeTimelineRequest(raw []byte, request *timeline.Request) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(request); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("request must be one JSON object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	revision, found := fields["expected_revision"]
	if !found || bytes.Equal(bytes.TrimSpace(revision), []byte("null")) {
		return errors.New("expected_revision is required")
	}
	return nil
}
