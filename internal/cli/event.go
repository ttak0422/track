package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"os"

	"github.com/ttak0422/track/internal/track/calendarevent"
	"github.com/ttak0422/track/internal/track/config"
)

func cmdEvent(args []string) int {
	if len(args) == 0 {
		return fail("event requires list, get or save")
	}
	verb := args[0]
	if verb != "list" && verb != "get" && verb != "save" {
		return fail("unknown event command %q", verb)
	}
	fs := flag.NewFlagSet("event "+verb, flag.ContinueOnError)
	id := fs.String("id", "", "canonical event UUID")
	deleted := fs.Bool("deleted", false, "include deleted events (list only)")
	expect := fs.Int64("expect", -1, "expected revision; 0 creates, positive updates (save only)")
	operation := fs.String("operation", "", "idempotent operation UUID (save only)")
	if code, ok := parseArgs(fs, args[1:]); !ok {
		return code
	}
	if fs.NArg() != 0 {
		return fail("event %s takes no positional arguments", verb)
	}
	if verb == "list" && (*id != "" || *expect != -1 || *operation != "") {
		return fail("event list only accepts --deleted")
	}
	if verb != "list" && *id == "" {
		return fail("--id is required")
	}
	if verb != "list" && *deleted {
		return fail("--deleted is only for event list; save deleted in the JSON payload")
	}
	if verb == "get" && (*expect != -1 || *operation != "") {
		return fail("event get only accepts --id")
	}
	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	switch verb {
	case "list":
		events, err := calendarevent.List(cfg, *deleted)
		if err != nil {
			return fail("%v", err)
		}
		return emit(map[string]any{"events": events})
	case "get":
		event, err := calendarevent.Get(cfg, *id)
		if err != nil {
			return fail("%v", err)
		}
		return emit(map[string]any{"event": event})
	default:
		if *expect < 0 || *operation == "" {
			return fail("save requires --expect N and --operation UUID")
		}
		raw, err := io.ReadAll(io.LimitReader(os.Stdin, calendarevent.MaxRequestBytes+1))
		if err != nil {
			return fail("read event JSON: %v", err)
		}
		if len(raw) > calendarevent.MaxRequestBytes {
			return fail("event JSON exceeds %d bytes", calendarevent.MaxRequestBytes)
		}
		var fields calendarevent.Fields
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&fields); err != nil {
			return fail("decode event JSON: %v", err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return fail("event input must be one JSON object")
		}
		event, err := calendarevent.Save(cfg, *id, calendarevent.WriteRequest{Fields: fields, ExpectedRevision: *expect, OperationID: *operation})
		if err != nil {
			return fail("%v", err)
		}
		return emit(map[string]any{"event": event})
	}
}
