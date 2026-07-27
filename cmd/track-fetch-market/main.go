// track-fetch-market converts J-Quants daily quotes into Canonical Data Model price JSONL — one
// OHLCV bar per line (see docs/spec/fetch.md for the contract it implements). It is independent of
// the track CLI: data goes to stdout (or --out), diagnostics to stderr, and every record is
// validated against the price kind before anything is written.
//
// Usage:
//
//	TRACK_JQUANTS_REFRESH_TOKEN=... track-fetch-market --code <issue code>
//	  [--from YYYY-MM-DD] [--to YYYY-MM-DD] [--entity <s>] [--out <file>]
//	  [--api-base <url>] [--timeout <dur>]
//
// The refresh token comes from the environment, never a flag, so it stays out of shell history and
// process listings. With --out the JSONL is written to the file (conventionally the vault's data/
// directory, which the web workspace watches for live re-renders) and a JSON summary is printed to
// stdout, matching the track CLI's result style.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/fetch/jquants"
	"github.com/ttak0422/track/internal/track/dataset"
)

// tokenEnv names the environment variable carrying the J-Quants refresh token.
const tokenEnv = "TRACK_JQUANTS_REFRESH_TOKEN"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("track-fetch-market", flag.ContinueOnError)
	fs.SetOutput(stderr)
	code := fs.String("code", "", "issue code (as listed by the exchange, e.g. a 4- or 5-digit stock code)")
	from := fs.String("from", "", "first day of the range, YYYY-MM-DD (open-ended when empty)")
	to := fs.String("to", "", "last day of the range, YYYY-MM-DD (open-ended when empty)")
	entity := fs.String("entity", "", "entity value stamped on every bar (defaults to the code)")
	out := fs.String("out", "", "write JSONL to this file instead of stdout (prints a JSON summary)")
	apiBase := fs.String("api-base", jquants.DefaultBaseURL, "API root (override for tests or a proxy)")
	timeout := fs.Duration("timeout", 30*time.Second, "HTTP timeout per request")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *code == "" {
		fmt.Fprintln(stderr, "track-fetch-market: --code is required")
		fs.Usage()
		return 2
	}
	for name, v := range map[string]string{"--from": *from, "--to": *to} {
		if v == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", v); err != nil {
			fmt.Fprintf(stderr, "track-fetch-market: %s must be YYYY-MM-DD, got %q\n", name, v)
			return 2
		}
	}
	refresh := os.Getenv(tokenEnv)
	if refresh == "" {
		fmt.Fprintf(stderr, "track-fetch-market: set %s to your J-Quants refresh token\n", tokenEnv)
		return 2
	}

	client := &jquants.Client{BaseURL: *apiBase, HTTP: &http.Client{Timeout: *timeout}}
	idToken, err := client.IDToken(refresh)
	if err != nil {
		return fail(stderr, err)
	}
	bars, skipped, err := client.DailyQuotes(idToken, *code, *from, *to)
	if err != nil {
		return fail(stderr, err)
	}
	if skipped > 0 {
		fmt.Fprintf(stderr, "track-fetch-market: skipped %d day(s) without a full price set\n", skipped)
	}
	ent := *entity
	if ent == "" {
		ent = *code
	}
	records, err := jquants.Prices(bars, ent)
	if err != nil {
		return fail(stderr, err)
	}

	var jsonl strings.Builder
	for _, rec := range records {
		line, err := json.Marshal(rec)
		if err != nil {
			return fail(stderr, err)
		}
		jsonl.Write(line)
		jsonl.WriteByte('\n')
	}

	if *out == "" {
		fmt.Fprint(stdout, jsonl.String())
		return 0
	}
	if err := os.WriteFile(*out, []byte(jsonl.String()), 0o644); err != nil {
		return fail(stderr, err)
	}
	summary, _ := json.Marshal(map[string]any{
		"path": *out, "kind": string(dataset.KindPrice), "records": len(records), "skipped": skipped,
	})
	fmt.Fprintln(stdout, string(summary))
	return 0
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "track-fetch-market: %v\n", err)
	return 1
}
