package webui

// Internal dispatch only: no CLI, listener, ordinary-vault admission or new
// filesystem authority. The caller owns process lifetime and bootstrap cleanup.
import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ttak0422/track/internal/track/config"
)

var errBoundLaunchProtocol = errors.New("bound launch request rejected")

type boundLaunchContext struct {
	Cwd         *string         `json:"cwd"`
	Pwd         json.RawMessage `json:"pwd"`
	Environment *[]string       `json:"environment"`
}
type boundLaunchSelector struct {
	Name *string `json:"name"`
	Path *string `json:"path"`
}
type boundLaunchRequest struct {
	Version     *int                 `json:"version"`
	Mode        *string              `json:"mode"`
	Selector    *boundLaunchSelector `json:"selector"`
	PrivateRoot *string              `json:"private_root"`
	Context     *boundLaunchContext  `json:"context"`
	Revision    *string              `json:"revision"`
	Nonce       *string              `json:"nonce"`
}
type boundLaunchResult struct {
	Manifest  *config.ReadOnlyLaunchManifest
	Bootstrap *Bootstrap
}

func boundText(s string, limit int) bool {
	return len(s) <= limit && utf8.ValidString(s) && !strings.ContainsFunc(s, unicode.IsControl)
}
func boundPath(s string) bool {
	return boundText(s, 4096) && filepath.IsAbs(s) && filepath.Clean(s) == s
}
func boundHex(s string, min, max int, lower bool) bool {
	if len(s) < min || len(s) > max || (lower && s != strings.ToLower(s)) {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// Reject duplicate object keys at every level before typed decoding. Both
// allocation and recursion are bounded independently of the Go JSON decoder.
func boundJSONValue(d *json.Decoder, depth int) error {
	if depth > 64 {
		return errBoundLaunchProtocol
	}
	token, err := d.Token()
	if err != nil {
		return errBoundLaunchProtocol
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return errBoundLaunchProtocol
			}
			s, ok := key.(string)
			if !ok || seen[s] || strings.ContainsFunc(s, func(c rune) bool { return !(c >= 'a' && c <= 'z' || c == '_') }) {
				return errBoundLaunchProtocol
			}
			seen[s] = true
			if err = boundJSONValue(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err = boundJSONValue(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return errBoundLaunchProtocol
	}
	end, err := d.Token()
	if err != nil || (delim == '{' && end != json.Delim('}')) || (delim == '[' && end != json.Delim(']')) {
		return errBoundLaunchProtocol
	}
	return nil
}
func parseBoundLaunch(raw []byte) (boundLaunchRequest, error) {
	var r boundLaunchRequest
	if len(raw) == 0 || len(raw) > 256<<10 || !utf8.Valid(raw) {
		return r, errBoundLaunchProtocol
	}
	scan := json.NewDecoder(bytes.NewReader(raw))
	if boundJSONValue(scan, 0) != nil {
		return r, errBoundLaunchProtocol
	}
	if _, err := scan.Token(); err != io.EOF {
		return r, errBoundLaunchProtocol
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&r) != nil {
		return r, errBoundLaunchProtocol
	}
	if r.Version == nil || *r.Version != 1 || r.Mode == nil || r.Selector == nil || r.Selector.Name == nil || r.Selector.Path == nil || r.PrivateRoot == nil || r.Context == nil || r.Context.Cwd == nil || r.Context.Environment == nil || len(r.Context.Pwd) == 0 || r.Revision == nil || r.Nonce == nil {
		return r, errBoundLaunchProtocol
	}
	if !boundPath(*r.PrivateRoot) || (!boundText(*r.Context.Cwd, 4096) || !filepath.IsAbs(*r.Context.Cwd)) || !boundHex(*r.Nonce, 32, 128, false) {
		return r, errBoundLaunchProtocol
	}
	name, path := *r.Selector.Name, *r.Selector.Path
	if !boundText(name, 256) || !boundText(path, 4096) || ((name != "" && path != "") || (name == "" && path == "")) {
		return r, errBoundLaunchProtocol
	}
	if name != "" && strings.ContainsFunc(name, func(c rune) bool { return !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') }) {
		return r, errBoundLaunchProtocol
	}
	switch *r.Mode {
	case "preflight":
		if *r.Revision != "" {
			return r, errBoundLaunchProtocol
		}
	case "serve":
		if !boundHex(*r.Revision, 64, 64, true) {
			return r, errBoundLaunchProtocol
		}
	default:
		return r, errBoundLaunchProtocol
	}
	var pwd *string
	if json.Unmarshal(r.Context.Pwd, &pwd) != nil || (pwd != nil && !boundText(*pwd, 4096)) {
		return r, errBoundLaunchProtocol
	}
	values := *r.Context.Environment
	if len(values) > 256 || len(strings.Join(values, "\n")) > 64<<10 {
		return r, errBoundLaunchProtocol
	}
	seen := map[string]bool{}
	home := false
	for i, entry := range values {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || !boundText(entry, 64<<10) || (key != "HOME" && !strings.HasPrefix(key, "TRACK_") && !strings.HasPrefix(key, "XDG_")) || seen[key] || (i > 0 && values[i-1] >= entry) {
			return r, errBoundLaunchProtocol
		}
		seen[key] = true
		if key == "HOME" {
			home = value != ""
		}
		if (key == "HOME" || key == "XDG_CACHE_HOME" || key == "XDG_CONFIG_HOME") && len(value) > 4096 {
			return r, errBoundLaunchProtocol
		}
	}
	if !home {
		return r, errBoundLaunchProtocol
	}
	return r, nil
}
func checkBoundLaunchContext(ctx context.Context, r boundLaunchRequest) error {
	if ctx.Err() != nil {
		return errBoundLaunchProtocol
	}
	cwd, err := os.Getwd()
	if err != nil || cwd != *r.Context.Cwd {
		return errBoundLaunchProtocol
	}
	var pwd *string
	if json.Unmarshal(r.Context.Pwd, &pwd) != nil {
		return errBoundLaunchProtocol
	}
	actual, exists := os.LookupEnv("PWD")
	if (pwd != nil) != exists || (pwd != nil && *pwd != actual) {
		return errBoundLaunchProtocol
	}
	values := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key == "HOME" || strings.HasPrefix(key, "TRACK_") || strings.HasPrefix(key, "XDG_") {
			values = append(values, entry)
		}
	}
	sort.Strings(values)
	if !reflect.DeepEqual(values, *r.Context.Environment) {
		return errBoundLaunchProtocol
	}
	// Momentary ownership check only. The bridge retains its parent watcher.
	pid, err := strconv.Atoi(os.Getenv("TRACK_WEB_DESKTOP_PARENT_PID"))
	if err != nil || pid <= 1 || pid != os.Getppid() || !boundHex(os.Getenv("TRACK_WEB_DESKTOP_READY_TOKEN"), 64, 64, false) {
		return errBoundLaunchProtocol
	}
	return nil
}
func dispatchBoundLaunch(ctx context.Context, raw []byte, lease func(*config.Config) (func(), error)) (boundLaunchResult, error) {
	var out boundLaunchResult
	r, err := parseBoundLaunch(raw)
	if err != nil {
		return out, errBoundLaunchProtocol
	}
	if checkBoundLaunchContext(ctx, r) != nil {
		return out, errBoundLaunchProtocol
	}
	selector := config.LaunchSelector{Name: *r.Selector.Name, Path: *r.Selector.Path}
	if *r.Mode == "preflight" {
		m, e := config.ResolveReadOnlyLaunchManifest(ctx, selector, *r.PrivateRoot)
		err = e
		if e == nil {
			out.Manifest = &m
		}
	} else {
		out.Bootstrap, err = newManifestBoundBootstrap(ctx, selector, *r.PrivateRoot, *r.Revision, *r.Nonce, lease)
	}
	if err != nil || checkBoundLaunchContext(ctx, r) != nil {
		if out.Bootstrap != nil {
			out.Bootstrap.Close()
		}
		return boundLaunchResult{}, errBoundLaunchProtocol
	}
	return out, nil
}
