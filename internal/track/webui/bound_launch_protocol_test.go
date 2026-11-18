//go:build linux || darwin

package webui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
)

func boundProtocolWire(t *testing.T, f boundFixture, mode, revision string) []byte {
	t.Helper()
	values := []string{}
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		if k == "HOME" || strings.HasPrefix(k, "TRACK_") || strings.HasPrefix(k, "XDG_") {
			values = append(values, e)
		}
	}
	sort.Strings(values)
	var pwd any
	if p, ok := os.LookupEnv("PWD"); ok {
		pwd = p
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"version": 1, "mode": mode, "selector": f.selector, "private_root": f.private, "context": map[string]any{"cwd": cwd, "pwd": pwd, "environment": values}, "revision": revision, "nonce": boundProcessNonce})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func boundProtocolAlter(t *testing.T, raw []byte, change func(map[string]any)) []byte {
	t.Helper()
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		t.Fatal("test request")
	}
	change(value)
	out, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestBoundLaunchProtocolRejectsMalformed(t *testing.T) {
	raw := []byte(`{"version":1,"mode":"preflight","selector":{"name":"work","path":""},"private_root":"/private","context":{"cwd":"/cwd","pwd":null,"environment":["HOME=/home"]},"revision":"","nonce":"0123456789abcdef0123456789abcdef"}`)
	if _, err := parseBoundLaunch(raw); err != nil {
		t.Fatal(err)
	}
	cases := [][]byte{nil, append(append([]byte{}, raw...), []byte(` {}`)...), []byte(strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66)), []byte(strings.Repeat(" ", 256<<10+1)), []byte(strings.Replace(string(raw), `"version":1`, `"version":1,"version":1`, 1)), []byte(strings.Replace(string(raw), `"name":"work"`, `"name":"work","name":"work"`, 1)), []byte(strings.Replace(string(raw), `"pwd":null`, `"pwd":null,"pwd":null`, 1))}
	changes := []func(map[string]any){
		func(v map[string]any) { delete(v, "nonce") }, func(v map[string]any) { v["revision"] = nil }, func(v map[string]any) { v["unexpected"] = true }, func(v map[string]any) { v["mode"] = "capture" }, func(v map[string]any) { v["mode"] = "serve" }, func(v map[string]any) { v["revision"] = strings.Repeat("a", 64) }, func(v map[string]any) { v["nonce"] = "short" },
		func(v map[string]any) { delete(v["context"].(map[string]any), "pwd") }, func(v map[string]any) { v["context"].(map[string]any)["pwd"] = 42 }, func(v map[string]any) { v["context"].(map[string]any)["extra"] = true }, func(v map[string]any) { v["selector"].(map[string]any)["extra"] = true }, func(v map[string]any) { v["context"].(map[string]any)["environment"] = nil },
		func(v map[string]any) {
			v["context"].(map[string]any)["environment"] = []string{"HOME=/home", "HOME=/home"}
		}, func(v map[string]any) {
			v["context"].(map[string]any)["environment"] = []string{"XDG_A=", "HOME=/home"}
		}, func(v map[string]any) { v["context"].(map[string]any)["environment"] = []string{"HOME="} }, func(v map[string]any) {
			v["context"].(map[string]any)["environment"] = []string{"HOME=/home", "PATH=x"}
		}, func(v map[string]any) {
			v["context"].(map[string]any)["environment"] = []string{"HOME=/home", "TRACK_X=a\nb"}
		},
	}
	for _, change := range changes {
		cases = append(cases, boundProtocolAlter(t, raw, change))
	}
	for i, raw := range cases {
		if _, err := parseBoundLaunch(raw); !errors.Is(err, errBoundLaunchProtocol) {
			t.Fatalf("case %d accepted", i)
		}
	}
}
func TestBoundLaunchProtocolOwnedContextAndDispatch(t *testing.T) {
	if boundProcessChild(t) {
		return
	}
	f := newBoundFixture(t, true)
	t.Setenv("TRACK_WEB_DESKTOP_PARENT_PID", strconv.Itoa(os.Getppid()))
	t.Setenv("TRACK_WEB_DESKTOP_READY_TOKEN", strings.Repeat("a", 64))
	t.Setenv("TRACK_UNKNOWN_CONTEXT", "")
	t.Setenv("PWD", f.cwd)
	raw := boundProtocolWire(t, f, "preflight", "")
	before := boundInventory(t, f, true)
	leases := 0
	lease := func(*config.Config) (func(), error) { leases++; return func() {}, nil }
	rejected := func(raw []byte, ctx context.Context) {
		t.Helper()
		out, err := dispatchBoundLaunch(ctx, raw, lease)
		if !errors.Is(err, errBoundLaunchProtocol) || out.Manifest != nil || out.Bootstrap != nil {
			t.Fatal("request was not rejected")
		}
		if leases != 0 {
			t.Fatal("rejected request acquired lease")
		}
		boundUnchanged(t, f, before, true)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	rejected(raw, cancelled)
	for _, change := range []func(map[string]any){func(v map[string]any) { v["context"].(map[string]any)["cwd"] = "/different" }, func(v map[string]any) { v["context"].(map[string]any)["pwd"] = nil }, func(v map[string]any) { v["context"].(map[string]any)["environment"] = []string{"HOME=/different"} }} {
		rejected(boundProtocolAlter(t, raw, change), context.Background())
	}
	for _, entry := range [][2]string{{"TRACK_WEB_DESKTOP_PARENT_PID", "1"}, {"TRACK_WEB_DESKTOP_READY_TOKEN", "invalid"}, {"TRACK_UNKNOWN_CONTEXT", "changed"}, {"PWD", ""}} {
		t.Run(entry[0], func(t *testing.T) {
			t.Setenv(entry[0], entry[1])
			rejected(raw, context.Background())
			if strings.HasPrefix(entry[0], "TRACK_WEB_DESKTOP_") {
				rejected(boundProtocolWire(t, f, "preflight", ""), context.Background())
			}
		})
	}
	preflight, err := dispatchBoundLaunch(context.Background(), raw, lease)
	if err != nil || preflight.Manifest == nil || preflight.Bootstrap != nil {
		t.Fatal("preflight failed", err)
	}
	boundUnchanged(t, f, before, true)
	wrong := boundProtocolWire(t, f, "serve", strings.Repeat("0", 64))
	rejected(wrong, context.Background())
	served, err := dispatchBoundLaunch(context.Background(), boundProtocolWire(t, f, "serve", preflight.Manifest.Revision), lease)
	if err != nil || served.Manifest != nil || served.Bootstrap == nil {
		t.Fatal("serve failed", err)
	}
	served.Bootstrap.Close()
	if leases != 0 {
		t.Fatal("dispatch started asynchronous lease work")
	}
	boundUnchanged(t, f, before, true)
	// The same context checker is applied after work; presence of empty must not
	// compare equal to absent. No timing-dependent environment mutation race.
	request, err := parseBoundLaunch(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Unsetenv("TRACK_UNKNOWN_CONTEXT"); err != nil {
		t.Fatal(err)
	}
	if checkBoundLaunchContext(context.Background(), request) == nil {
		t.Fatal("absent equated with empty")
	}
}

func TestBoundLaunchProtocolContextBounds(t *testing.T) {
	raw := []byte(`{"version":1,"mode":"preflight","selector":{"name":"","path":"~/relative"},"private_root":"/private","context":{"cwd":"/cwd","pwd":"","environment":["HOME=x"]},"revision":"","nonce":"0123456789abcdef0123456789abcdef"}`)
	withEnv := func(values []string) []byte {
		return boundProtocolAlter(t, raw, func(v map[string]any) { v["context"].(map[string]any)["environment"] = values })
	}
	values := []string{"HOME=x"}
	for i := 0; i < 255; i++ {
		values = append(values, "TRACK_"+strconv.Itoa(i)+"=")
	}
	sort.Strings(values)
	if _, err := parseBoundLaunch(withEnv(values)); err != nil {
		t.Fatal("256 entries rejected")
	}
	values = append(values, "XDG_OVER=")
	sort.Strings(values)
	if _, err := parseBoundLaunch(withEnv(values)); err == nil {
		t.Fatal("257 entries accepted")
	}
	payload := strings.Repeat("x", (64<<10)-len("HOME=x\nTRACK_X="))
	if _, err := parseBoundLaunch(withEnv([]string{"HOME=x", "TRACK_X=" + payload})); err != nil {
		t.Fatal("exact byte bound rejected")
	}
	if _, err := parseBoundLaunch(withEnv([]string{"HOME=x", "TRACK_X=" + payload + "x"})); err == nil {
		t.Fatal("byte overflow accepted")
	}
	for _, key := range []string{"HOME", "XDG_CACHE_HOME", "XDG_CONFIG_HOME"} {
		values := []string{"HOME=x", key + "=" + strings.Repeat("x", 4096)}
		if key == "HOME" {
			values = values[1:]
		}
		sort.Strings(values)
		if _, err := parseBoundLaunch(withEnv(values)); err != nil {
			t.Fatal("path bound rejected")
		}
		values[len(values)-1] += "x"
		if _, err := parseBoundLaunch(withEnv(values)); err == nil {
			t.Fatal("path overflow accepted")
		}
	}
	for _, value := range []any{strings.Repeat("x", 4097), "line\nbreak"} {
		if _, err := parseBoundLaunch(boundProtocolAlter(t, raw, func(v map[string]any) { v["context"].(map[string]any)["pwd"] = value })); err == nil {
			t.Fatal("invalid PWD accepted")
		}
	}
	if _, err := parseBoundLaunch(raw); err != nil {
		t.Fatal("raw selector or empty PWD rejected")
	}
}

func TestBoundLaunchProtocolPreservesLexicalCwdButRequiresSelector(t *testing.T) {
	raw := []byte(`{"version":1,"mode":"preflight","selector":{"name":"","path":"../vault"},"private_root":"/private","context":{"cwd":"/alias/../cwd//","pwd":"/alias/../cwd//","environment":["HOME=x"]},"revision":"","nonce":"0123456789abcdef0123456789abcdef"}`)
	request, err := parseBoundLaunch(raw)
	if err != nil || *request.Context.Cwd != "/alias/../cwd//" {
		t.Fatal("lexical cwd changed or rejected")
	}
	raw = boundProtocolAlter(t, raw, func(v map[string]any) { v["selector"].(map[string]any)["path"] = "" })
	if _, err := parseBoundLaunch(raw); err == nil {
		t.Fatal("implicit selector accepted")
	}
}

func TestBoundLaunchProtocolRejectsCaseFoldedFieldNames(t *testing.T) {
	raw := `{"version":1,"mode":"preflight","selector":{"name":"work","path":""},"private_root":"/private","context":{"cwd":"/cwd","pwd":null,"environment":["HOME=x"]},"revision":"","nonce":"0123456789abcdef0123456789abcdef"}`
	for _, pair := range [][2]string{{`"version":1`, `"Version":1`}, {`"version":1`, `"version":1,"Version":1`}, {`"cwd":`, `"Cwd":`}, {`"path":`, `"PATH":`}, {`"pwd":`, `"Pwd":`}} {
		if _, err := parseBoundLaunch([]byte(strings.Replace(raw, pair[0], pair[1], 1))); err == nil {
			t.Fatal("case folded field accepted")
		}
	}
}

func TestBoundLaunchProtocolRejectsUnicodeFieldAliases(t *testing.T) {
	raw := `{"version":1,"mode":"preflight","selector":{"name":"work","path":""},"private_root":"/private","context":{"cwd":"/cwd","pwd":null,"environment":["HOME=x"]},"revision":"","nonce":"0123456789abcdef0123456789abcdef"}`
	for _, pair := range [][2]string{{`"version":`, `"verſion":`}, {`"selector":`, `"ſelector":`}, {`"version":`, `"ver\u017fion":`}, {`"selector":`, `"\u017felector":`}, {`"version":1`, `"version":1,"ver\u017fion":1`}} {
		if _, err := parseBoundLaunch([]byte(strings.Replace(raw, pair[0], pair[1], 1))); err == nil {
			t.Fatal("Unicode schema alias accepted")
		}
	}
}

func TestBoundLaunchProtocolRevisionDigitsAreNotSchemaKeys(t *testing.T) {
	// Regression for accidentally applying the schema-key ASCII rule to a hex
	// value: serve must admit digits before any context or filesystem dispatch.
	raw := []byte(`{"version":1,"mode":"serve","selector":{"name":"work","path":""},"private_root":"/private","context":{"cwd":"/cwd","pwd":null,"environment":["HOME=x"]},"revision":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","nonce":"0123456789abcdef0123456789abcdef"}`)
	if _, err := parseBoundLaunch(raw); err != nil {
		t.Fatal("lowercase digit-bearing revision rejected at parse boundary")
	}
	upper := strings.Replace(string(raw), "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF", 1)
	if _, err := parseBoundLaunch([]byte(upper)); err == nil {
		t.Fatal("uppercase revision accepted")
	}
}
