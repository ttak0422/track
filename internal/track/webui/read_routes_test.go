package webui

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/store"
)

// Keep the classification independent of registration. Adding a mux route
// without an explicit allow/deny decision must fail, including dynamic patterns.
func TestInternalReadRouteInventory(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "webui.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "routes" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc") {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Fatal("route registration must have an explicitly inventoried literal pattern")
			}
			route, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := internalReadRoutes[route]; !ok {
				t.Errorf("unclassified route %s", route)
			}
			found[route] = true
			return true
		})
	}
	for route := range internalReadRoutes {
		if !found[route] {
			t.Errorf("classification without registered route %s", route)
		}
	}
	if len(found) == 0 {
		t.Fatal("registration inventory was not inspected")
	}
	// Bootstrap's special dispatch and the pre-mux interception are not mux
	// registrations. Any new literal path there also needs a classification.
	for _, source := range []struct{ file, function string }{{"bootstrap.go", "serveHTTP"}, {"webui.go", "Handler"}} {
		file, err := parser.ParseFile(token.NewFileSet(), source.file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		inspected := false
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != source.function {
				continue
			}
			inspected = true
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				value, _ := strconv.Unquote(lit.Value)
				if !strings.HasPrefix(value, "/") || value == "/api/" {
					return true
				} // namespace check, not dispatch
				_, api := internalReadRoutes[value]
				_, special := internalReadSpecialRoutes[value]
				if !api && !special {
					t.Errorf("unclassified special route %s in %s", value, source.function)
				}
				return true
			})
		}
		if !inspected {
			t.Fatalf("special dispatch %s not inspected", source.function)
		}
	}
	for _, path := range []string{"/api/storage", "/apps", "/apps/", staticAppsRoute} {
		if _, ok := internalReadSpecialRoutes[path]; !ok {
			t.Errorf("missing special route %s", path)
		}
	}
}

func TestInternalReadExactMethodsAndPaths(t *testing.T) {
	gets := strings.Fields("/api/capabilities /api/search /api/notes /api/activity /api/agenda /api/journal/lookup /api/resolve /api/note /api/note/meta /api/tasks /api/calendar-activity/range /api/calendar-events/range /api/calendar-events/day /api/calendar-events /api/calendar-event /api/hierarchy /api/graph/local /api/graph /api/events /api/vaults /api/follow /api/markdown /api/asset")
	expected := map[string]map[string]bool{}
	for _, path := range gets {
		expected[path] = map[string]bool{"GET": true}
	}
	expected["/api/asset"]["HEAD"] = true
	for _, path := range []string{"/api/render", "/api/viewspec", "/api/follow", "/api/markdown", "/api/markdown/consume"} {
		if expected[path] == nil {
			expected[path] = map[string]bool{}
		}
		expected[path]["POST"] = true
	}
	for path := range internalReadRoutes {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE", "CONNECT", "get", ""} {
			req := httptest.NewRequest("GET", "http://localhost"+path, nil)
			req.Method = method
			if got := internalReadRequestAllowed(req, false); got != expected[path][method] {
				t.Errorf("%s %s allowed=%v", method, path, got)
			}
		}
	}
	for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE", "OPTIONS"} {
		req := httptest.NewRequest(method, "http://localhost/api/storage", nil)
		if internalReadRequestAllowed(req, false) {
			t.Fatal("bootstrap-only storage authorized on Server fallback")
		}
		if got := internalReadRequestAllowed(req, true); got != (method == "GET" || method == "POST") {
			t.Errorf("storage method %s=%v", method, got)
		}
	}
	for _, path := range []string{"/api/notes/", "/api//notes", "/api/x/../notes", "/api/./notes", "/api/%6eotes", "/api%2fnotes", "/%61pi/notes", "/api/notes%2f", "/api/notes/extra", "/api/unknown", "/api", "/apps", "/apps/app/", "/__track/apps/launch/app/", "/", "/assets/app.js"} {
		req := httptest.NewRequest("GET", "http://localhost"+path, nil)
		for _, bootstrap := range []bool{false, true} {
			if internalReadRequestAllowed(req, bootstrap) {
				t.Errorf("alias/unknown %s authorized", path)
			}
		}
	}
	req := httptest.NewRequest("GET", "http://localhost/api/notes?vault=work", nil)
	if !internalReadRequestAllowed(req, true) {
		t.Fatal("query selector changed exact route authority")
	}
	req.URL.RawPath = "/api/notes"
	if internalReadRequestAllowed(req, true) {
		t.Fatal("explicit RawPath accepted")
	}
	req.URL.RawPath = ""
	req.URL.Opaque = "//localhost/api/notes"
	if internalReadRequestAllowed(req, true) {
		t.Fatal("opaque target accepted")
	}
}

type unreadablePolicyBody struct{ t *testing.T }

func (b unreadablePolicyBody) Read([]byte) (int, error) {
	b.t.Fatal("denied request decoded/read body")
	return 0, nil
}
func (b unreadablePolicyBody) Close() error { return nil }

func TestInternalReadDeniedBeforeDispatch(t *testing.T) {
	p := &readPolicy{}
	s := newServerWithReadPolicy(&config.Config{Provisional: true}, nil, true, p)
	defer s.closeViews()
	// Any dispatch, including unknown routes falling through to the SPA, fails.
	var dispatched atomic.Int32
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("/", func(http.ResponseWriter, *http.Request) { dispatched.Add(1) })
	var loads atomic.Int32
	b := newBootstrapWithReadPolicy(context.Background(), func(context.Context, string, bool) (*config.Config, error) {
		loads.Add(1)
		t.Error("denied request loaded configuration")
		return nil, nil
	}, store.Open, nil, p)
	defer b.Close()
	routes := []struct{ method, path string }{
		{"POST", "/api/note"}, {"PUT", "/api/note"}, {"DELETE", "/api/note"}, {"POST", "/api/journal"}, {"POST", "/api/note/meta"}, {"POST", "/api/note/read"}, {"POST", "/api/task"}, {"PUT", "/api/calendar-event"}, {"POST", "/api/asset"}, {"GET", "/api/ogp?url=https://example.invalid"}, {"GET", "/api/requests"}, {"GET", "/api/requests/id"}, {"POST", "/api/requests/id/run"}, {"GET", "/api/agents"}, {"POST", "/api/metrics-dashboard"}, {"GET", "/apps/app/"}, {"GET", "/__track/apps/launch/app/"}, {"GET", "/api/notes/"}, {"GET", "/api/%6eotes"}, {"GET", "/api/unknown"}, {"HEAD", "/api/note"}, {"DELETE", "/api/storage"}, {"POST", "/api/storage/"},
	}
	for _, tc := range routes {
		for _, h := range []http.Handler{s.Handler(), b} {
			req := httptest.NewRequest(tc.method, "http://localhost"+tc.path, nil)
			q := req.URL.Query()
			q.Set("vault", "unserved")
			req.URL.RawQuery = q.Encode()
			req.Body = unreadablePolicyBody{t}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusForbidden {
				t.Errorf("denied %s %s got %d: %s", tc.method, tc.path, w.Code, w.Body)
			}
		}
	}
	for _, path := range []string{"/", "/__track/apps/launch/app/", "/api/notes"} {
		w := httptest.NewRecorder()
		s.staticAppsHandler().ServeHTTP(w, httptest.NewRequest("GET", "http://localhost"+path, nil))
		if w.Code != 403 {
			t.Fatalf("secondary listener allowed %s: %d", path, w.Code)
		}
	}
	if dispatched.Load() != 0 || loads.Load() != 0 || len(s.views) != 0 {
		t.Fatal("denial reached dispatch/configuration/vault resolution")
	}
	// Special retry is allowed only behind the normal Host/Origin guard, before
	// any initial backend exists. No bogus body is needed for retry.
	for _, kind := range []string{"Host", "Origin", "Sec-Fetch-Site", "Referer"} {
		req := httptest.NewRequest("POST", "http://localhost/api/storage", nil)
		if kind == "Host" {
			req.Host = "evil.invalid"
		} else if kind == "Sec-Fetch-Site" {
			req.Header.Set(kind, "cross-site")
		} else {
			req.Header.Set(kind, "https://evil.invalid")
		}
		w := httptest.NewRecorder()
		b.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Errorf("retry guard %s=%d", kind, w.Code)
		}
	}
	if loads.Load() != 0 {
		t.Fatal("cross-origin retry started loader")
	}
}
