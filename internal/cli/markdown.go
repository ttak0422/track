package cli

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/markdownhandoff"
)

// defaultWebAddr matches the local workspace listener.
const defaultWebAddr = "127.0.0.1:8765"

func cmdMarkdown(args []string) int {
	fs := flag.NewFlagSet("markdown", flag.ContinueOnError)
	addr := fs.String("addr", defaultWebAddr, "running loopback workspace address")
	check := fs.Bool("check", false, "probe for a compatible workspace without reading files")
	if code, ok := parseArgs(fs, args, "track markdown [--addr HOST:PORT] [--check | -- FILE]\n\nHand a saved Markdown file and its containing folder to the read-only viewer.\nStart track web first; Neovim's :Track markdown starts or reuses it automatically."); !ok {
		return code
	}
	if (*check && fs.NArg() != 0) || (!*check && fs.NArg() != 1) {
		return fail("markdown: provide one saved Markdown file, or --check")
	}
	base, err := markdownBaseURL(*addr)
	if err != nil {
		return fail("markdown: %v", err)
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("redirect refused") }}
	defer client.CloseIdleConnections()
	response, err := client.Get(base + "/api/markdown")
	if err != nil {
		if *check {
			return emit(map[string]bool{"ready": false})
		}
		return fail("markdown: start track web on %s first: %v", *addr, err)
	}
	var probe struct {
		Protocol string `json:"protocol"`
	}
	err = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&probe)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || probe.Protocol != markdownhandoff.Protocol {
		return fail("markdown: workspace does not support Markdown handoff; update Track or choose another address")
	}
	if *check {
		return emit(map[string]bool{"ready": true})
	}
	snapshot, err := markdownhandoff.Read(fs.Arg(0))
	if err != nil {
		return fail("markdown: %v", err)
	}
	body, err := json.Marshal(snapshot)
	if err != nil || len(body) > markdownhandoff.MaxWireBytes {
		return fail("markdown: snapshot exceeds the transfer limit")
	}
	response, err = client.Post(base+"/api/markdown", "application/json", bytes.NewReader(body))
	if err != nil {
		return fail("markdown: %v", err)
	}
	defer response.Body.Close()
	var result struct {
		Route string `json:"route"`
		Error string `json:"error"`
	}
	err = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result)
	const prefix = "/markdown#handoff="
	token := strings.TrimPrefix(result.Route, prefix)
	_, tokenErr := hex.DecodeString(token)
	if response.StatusCode != http.StatusOK || err != nil || !strings.HasPrefix(result.Route, prefix) || len(token) != 64 || tokenErr != nil {
		return fail("markdown: handoff refused (%d): %s", response.StatusCode, result.Error)
	}
	return emit(map[string]string{"url": base + result.Route})
}

func markdownBaseURL(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	n, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("--addr must be a loopback HOST:PORT")
	}
	if host == "localhost" {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("Markdown handoff requires a loopback address")
	}
	return "http://" + net.JoinHostPort(host, port), nil
}
