package webui

import "net/http"

// Canonical-read-only capability inventory. Empty methods
// explicitly deny a registered route. Never infer authority from GET or from a
// prefix: some GETs fetch remote URLs, and some POSTs only compute local results
// or update bounded process memory. All index refreshes still require readPolicy.
var internalReadRoutes = map[string][]string{
	"/api/capabilities":            {http.MethodGet},
	"/api/search":                  {http.MethodGet},
	"/api/notes":                   {http.MethodGet},
	"/api/activity":                {http.MethodGet},
	"/api/agenda":                  {http.MethodGet},
	"/api/journal/lookup":          {http.MethodGet},
	"/api/journal":                 nil, // Creates a journal.
	"/api/resolve":                 {http.MethodGet},
	"/api/note":                    {http.MethodGet},
	"/api/note/meta":               {http.MethodGet},
	"/api/note/read":               nil, // Writes seen/read sidecar timestamps.
	"/api/tasks":                   {http.MethodGet},
	"/api/task":                    nil,
	"/api/calendar-activity/range": {http.MethodGet},
	"/api/calendar-events/range":   {http.MethodGet},
	"/api/calendar-events/day":     {http.MethodGet},
	"/api/calendar-events":         {http.MethodGet},
	"/api/calendar-event":          {http.MethodGet},
	"/api/render":                  {http.MethodPost}, // Local query/include/dashboard expansion.
	"/api/metrics-dashboard":       nil,               // Local compute, not required by this capability.
	"/api/viewspec":                {http.MethodPost}, // Inline or local data-file computation.
	"/api/asset":                   {http.MethodGet, http.MethodHead},
	"/api/timeline/projects":       nil, // Timeline is not part of the audited native read capability.
	"/api/timeline/project":        nil,
	"/api/timeline":                nil,
	"/api/ogp":                     nil, // External HTTP fetch, despite being GET.
	"/api/hierarchy":               {http.MethodGet},
	"/api/graph/local":             {http.MethodGet},
	"/api/graph":                   {http.MethodGet},
	"/api/follow":                  {http.MethodGet, http.MethodPost}, // Memory + SSE, after policy-safe refresh.
	"/api/markdown":                {http.MethodGet, http.MethodPost}, // Bounded loopback capability inbox.
	"/api/markdown/consume":        {http.MethodPost},                 // One-time in-memory consumption.
	"/api/events":                  {http.MethodGet},
	"/api/vaults":                  {http.MethodGet},
	"/api/requests":                nil, // Archived history is outside native scope.
	"/api/requests/{id}":           nil,
	"/api/agents":                  nil, // Retired routes.
	"/api/requests/{id}/{action}":  nil,
	"/":                            nil, // Embedded SPA/static fallback is not an API capability.
}

// Bootstrap and pre-mux/secondary-listener routes are inventoried separately:
// they must not silently acquire authority by bypassing Server.routes.
var internalReadSpecialRoutes = map[string][]string{
	"/api/storage":  {http.MethodGet, http.MethodPost}, // Retry preserves the captured immutable policy.
	"/apps":         nil,
	"/apps/":        nil,
	staticAppsRoute: nil,
}

func internalReadRequestAllowed(r *http.Request, bootstrap bool) bool {
	if r.URL == nil || r.URL.Opaque != "" || r.URL.RawPath != "" || r.URL.EscapedPath() != r.URL.Path {
		return false
	}
	// Exact paths reject trailing/double slashes, dot segments and encoded
	// aliases before ServeMux can redirect or reinterpret them.
	methods, ok := internalReadRoutes[r.URL.Path]
	if !ok && bootstrap {
		methods = internalReadSpecialRoutes[r.URL.Path]
	}
	for _, method := range methods {
		if r.Method == method {
			return true
		}
	}
	return false
}
