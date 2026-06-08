package webui

const indexHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>track web</title>
  <script>
    (function () {
      var theme = localStorage.getItem("track.theme") || "system";
      if (theme === "light" || theme === "dark") {
        document.documentElement.dataset.theme = theme;
      }
    })();
  </script>
  <link rel="stylesheet" href="/style.css">
</head>
<body>
  <main class="workspace">
    <aside class="sidebar">
      <header class="brand">
        <div>
          <h1><a id="home-link" class="home-link" href="/">track</a></h1>
          <p>Local graph workspace</p>
        </div>
        <div class="app-menu">
          <button id="menu-button" class="menu-button" type="button" aria-label="Open menu" aria-haspopup="true" aria-expanded="false">
            <span></span>
            <span></span>
            <span></span>
          </button>
          <div id="menu-panel" class="menu-panel" hidden>
            <section class="menu-section" aria-labelledby="theme-menu-label">
              <h2 id="theme-menu-label">Theme</h2>
              <div class="theme-switch" role="group" aria-label="Theme">
                <button type="button" data-theme-choice="system">System</button>
                <button type="button" data-theme-choice="light">Light</button>
                <button type="button" data-theme-choice="dark">Dark</button>
              </div>
            </section>
          </div>
        </div>
      </header>
      <div class="searchbar">
        <div class="searchbox">
          <div id="search-chips" class="search-chips" aria-label="Tag filters"></div>
          <input id="search" type="search" placeholder="Search notes" autocomplete="off">
        </div>
      </div>
      <div id="results" class="results" aria-live="polite"></div>
      <section class="graph-panel" aria-label="Local graph">
        <header class="graph-header">
          <h3>Local Graph</h3>
          <p id="graph-meta"></p>
        </header>
        <canvas id="graph"></canvas>
      </section>
    </aside>
    <section class="reader">
      <article id="note-body" class="note-body"></article>
      <section class="backlinks">
        <h3>Backlinks</h3>
        <div id="backlinks"></div>
      </section>
    </section>
  </main>
  <script src="/app.js"></script>
</body>
</html>
`

const styleCSS = `:root {
  color-scheme: light dark;
  --bg: #f7f7f4;
  --panel: #ffffff;
  --panel-soft: #f0f2ef;
  --text: #20231f;
  --muted: #687069;
  --line: #d9ddd5;
  --accent: #2f6f5e;
  --accent-strong: #174c40;
  --generated: #9a6718;
  --danger: #8a352b;
  font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}

:root[data-theme="dark"] {
  color-scheme: dark;
  --bg: #161814;
  --panel: #20231f;
  --panel-soft: #292d28;
  --text: #ecefe8;
  --muted: #a3aa9f;
  --line: #3a4039;
  --accent: #62b39b;
  --accent-strong: #8dd8c2;
  --generated: #e0b05f;
  --danger: #de766b;
}

:root[data-theme="light"] {
  color-scheme: light;
}

@media (prefers-color-scheme: dark) {
  :root:not([data-theme="light"]) {
    --bg: #161814;
    --panel: #20231f;
    --panel-soft: #292d28;
    --text: #ecefe8;
    --muted: #a3aa9f;
    --line: #3a4039;
    --accent: #62b39b;
    --accent-strong: #8dd8c2;
    --generated: #e0b05f;
    --danger: #de766b;
  }
}

* { box-sizing: border-box; }

html, body {
  margin: 0;
  height: 100%;
  background: var(--bg);
  color: var(--text);
}

button, input {
  font: inherit;
}

.workspace {
  display: grid;
  grid-template-columns: minmax(260px, 340px) minmax(380px, 1fr);
  height: 100vh;
  min-height: 560px;
}

.sidebar, .reader {
  min-width: 0;
  min-height: 0;
  border-right: 1px solid var(--line);
  background: var(--panel);
}

.sidebar {
  display: flex;
  flex-direction: column;
}

.brand {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  min-height: 76px;
  padding: 16px 18px;
  border-bottom: 1px solid var(--line);
}

.app-menu {
  position: relative;
  flex: 0 0 auto;
}

.home-link {
  color: inherit;
  text-decoration: none;
}

.home-link:hover {
  color: var(--accent-strong);
}

.menu-button {
  display: inline-grid;
  place-content: center;
  gap: 4px;
  width: 34px;
  height: 34px;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--text);
  background: var(--panel);
  cursor: pointer;
}

.menu-button:hover, .menu-button[aria-expanded="true"] {
  background: var(--panel-soft);
}

.menu-button span {
  display: block;
  width: 16px;
  height: 2px;
  border-radius: 2px;
  background: currentColor;
}

.menu-panel {
  position: absolute;
  z-index: 20;
  top: calc(100% + 8px);
  right: 0;
  width: 214px;
  border: 1px solid var(--line);
  border-radius: 6px;
  padding: 10px;
  background: var(--panel);
  box-shadow: 0 16px 38px color-mix(in srgb, #000 18%, transparent);
}

.menu-section h2 {
  margin: 0 0 8px;
  color: var(--muted);
  font-size: 12px;
  font-weight: 650;
}

.theme-switch {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  overflow: hidden;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--panel-soft);
}

.theme-switch button {
  min-width: 0;
  height: 30px;
  border: 0;
  border-right: 1px solid var(--line);
  padding: 0 6px;
  color: var(--muted);
  background: transparent;
  font-size: 11px;
  cursor: pointer;
}

.theme-switch button:last-child {
  border-right: 0;
}

.theme-switch button[aria-pressed="true"] {
  color: var(--text);
  background: var(--panel);
  font-weight: 650;
}

h1, h2, h3, p {
  margin: 0;
}

h1 {
  font-size: 22px;
  font-weight: 680;
}

h2 {
  font-size: 18px;
  font-weight: 660;
}

h3 {
  font-size: 14px;
  font-weight: 650;
  color: var(--muted);
}

p {
  color: var(--muted);
  font-size: 12px;
  line-height: 1.4;
}

.searchbar {
  padding: 12px;
  border-bottom: 1px solid var(--line);
}

.searchbox {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  width: 100%;
  min-height: 36px;
  border: 1px solid var(--line);
  border-radius: 6px;
  padding: 3px 7px;
  background: var(--panel);
}

.searchbox:focus-within {
  border-color: var(--accent);
}

.searchbox input {
  flex: 1 1 auto;
  min-width: 70px;
  height: 28px;
  border: 0;
  padding: 0 3px;
  background: transparent;
  color: var(--text);
  outline: none;
}

.search-chips {
  display: contents;
}

.search-chip {
  flex: 0 1 auto;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  max-width: 100%;
  min-height: 24px;
  border: 1px solid color-mix(in srgb, var(--accent) 50%, transparent);
  border-radius: 999px;
  padding: 0 8px;
  color: var(--accent-strong);
  background: var(--panel-soft);
  font-size: 12px;
  font-weight: 620;
  cursor: pointer;
}

.search-chip span:last-child {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.search-chip span:first-child {
  color: var(--muted);
}

.results {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
  padding: 6px;
}

.result {
  width: 100%;
  display: block;
  text-align: left;
  border: 0;
  border-radius: 6px;
  padding: 9px 10px;
  color: var(--text);
  background: transparent;
  cursor: pointer;
}

.result:hover, .result.active {
  background: var(--panel-soft);
}

.result-title {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  font-size: 14px;
  font-weight: 560;
}

.result-title span:first-child {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tag-list {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 6px;
  margin-top: 5px;
}

.tag {
  border: 0;
  padding: 0;
  color: var(--accent-strong);
  background: transparent;
  font-size: 11px;
  line-height: 1.35;
  font-weight: 560;
  cursor: pointer;
}

.tag:hover {
  text-decoration: underline;
  text-underline-offset: 2px;
}

.note-tags {
  margin: -2px 0 16px;
}

.note-tags .tag {
  font-size: 12px;
}

.result-meta {
  margin-top: 3px;
  color: var(--muted);
  font-size: 11px;
}

.badge {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  min-height: 20px;
  border: 1px solid color-mix(in srgb, var(--generated) 55%, transparent);
  border-radius: 999px;
  padding: 0 7px;
  color: var(--generated);
  font-size: 11px;
  font-weight: 620;
}

.hidden {
  display: none;
}

[hidden] {
  display: none !important;
}

.reader {
  display: flex;
  flex-direction: column;
}

.note-body {
  flex: 1;
  overflow: auto;
  padding: 22px 28px;
  line-height: 1.65;
}

.note-body h1, .note-body h2, .note-body h3 {
  margin: 20px 0 8px;
  color: var(--text);
}

.note-body h1 { font-size: 28px; }
.note-body h2 { font-size: 22px; }
.note-body h3 { font-size: 17px; }
.note-body p { margin: 10px 0; color: var(--text); font-size: 15px; }
.note-body pre {
  overflow: auto;
  border: 1px solid var(--line);
  border-radius: 6px;
  padding: 12px;
  background: var(--panel-soft);
}
.note-body code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}

.home-list {
  display: grid;
  gap: 12px;
  max-width: 760px;
  margin-top: 14px;
}

.home-note {
  display: block;
  color: var(--text);
  text-decoration: none;
}

.home-note:hover .home-note-title {
  color: var(--accent-strong);
  text-decoration: underline;
  text-underline-offset: 3px;
}

.home-note-title {
  font-size: 15px;
  font-weight: 650;
}

.home-summary {
  margin-top: 4px;
}

.note-preview {
  position: fixed;
  z-index: 60;
  width: min(380px, calc(100vw - 24px));
  max-height: min(520px, calc(100vh - 24px));
  overflow: auto;
  border: 1px solid var(--line);
  border-radius: 6px;
  padding: 14px 16px;
  background: var(--panel);
  box-shadow: 0 18px 42px color-mix(in srgb, #000 18%, transparent);
}

.note-preview-title {
  color: var(--text);
  font-size: 16px;
  font-weight: 680;
  line-height: 1.35;
}

.note-preview-body {
  margin-top: 10px;
}

.note-preview-body h1, .note-preview-body h2, .note-preview-body h3 {
  margin: 12px 0 6px;
  color: var(--text);
}

.note-preview-body h1 { font-size: 18px; }
.note-preview-body h2 { font-size: 16px; }
.note-preview-body h3 { font-size: 14px; }
.note-preview-body p { margin: 7px 0; color: var(--text); font-size: 13px; }
.note-preview-body pre {
  overflow: auto;
  border: 1px solid var(--line);
  border-radius: 6px;
  padding: 10px;
  background: var(--panel-soft);
}

.wiki-link {
  border: 0;
  padding: 0;
  color: var(--accent-strong);
  background: transparent;
  font: inherit;
  font-weight: 650;
  text-decoration: underline;
  text-decoration-thickness: 1px;
  text-underline-offset: 3px;
  cursor: pointer;
}

.wiki-link.unresolved {
  color: var(--danger);
  text-decoration-style: dotted;
}

.backlinks {
  border-top: 1px solid var(--line);
  padding: 14px 18px 18px;
  min-height: 116px;
  max-height: 28vh;
  overflow: auto;
}

.backlink {
  display: block;
  margin-top: 7px;
  color: var(--accent-strong);
  font-size: 14px;
  font-weight: 650;
  line-height: 1.45;
  text-decoration: underline;
  text-decoration-thickness: 1px;
  text-underline-offset: 3px;
}

.backlink:hover {
  color: var(--accent);
}

.graph-panel {
  flex: 0 0 360px;
  min-height: 260px;
  border-top: 1px solid var(--line);
  background: var(--panel);
}

.graph-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  min-height: 38px;
  padding: 10px 12px 8px;
}

.graph-header p {
  white-space: nowrap;
}

#graph {
  display: block;
  width: 100%;
  height: calc(100% - 38px);
  background: var(--panel);
  cursor: grab;
  touch-action: none;
}

#graph.dragging {
  cursor: grabbing;
}

.empty {
  padding: 16px;
  color: var(--muted);
  font-size: 13px;
}

@media (max-width: 980px) {
  .workspace {
    grid-template-columns: 1fr;
    grid-template-rows: 620px minmax(360px, 1fr);
    height: auto;
    min-height: 100vh;
  }
  .sidebar, .reader {
    border-right: 0;
    border-bottom: 1px solid var(--line);
  }
}
`

const appJS = `(function () {
  var state = {
    results: [],
    selectedID: null,
    activeTags: [],
    graph: null,
    nodes: [],
    edges: [],
    graphView: { x: 0, y: 0, scale: 1 },
    dragging: null,
    pointers: {},
    pinch: null,
    animation: null,
    preview: {
      panel: null,
      hideTimer: null,
      request: 0,
      cache: {}
    }
  };

  var el = {
    homeLink: document.getElementById("home-link"),
    search: document.getElementById("search"),
    searchChips: document.getElementById("search-chips"),
    results: document.getElementById("results"),
    body: document.getElementById("note-body"),
    backlinks: document.getElementById("backlinks"),
    graphMeta: document.getElementById("graph-meta"),
    canvas: document.getElementById("graph"),
    menuButton: document.getElementById("menu-button"),
    menuPanel: document.getElementById("menu-panel"),
    themeButtons: Array.prototype.slice.call(document.querySelectorAll("[data-theme-choice]"))
  };

  var systemTheme = window.matchMedia ? window.matchMedia("(prefers-color-scheme: dark)") : null;

  function themeMode() {
    var saved = localStorage.getItem("track.theme");
    return saved === "light" || saved === "dark" ? saved : "system";
  }

  function applyTheme(mode) {
    if (mode === "light" || mode === "dark") {
      document.documentElement.dataset.theme = mode;
      localStorage.setItem("track.theme", mode);
    } else {
      document.documentElement.removeAttribute("data-theme");
      localStorage.setItem("track.theme", "system");
      mode = "system";
    }
    el.themeButtons.forEach(function (button) {
      button.setAttribute("aria-pressed", button.dataset.themeChoice === mode ? "true" : "false");
    });
    drawGraph();
  }

  function setMenuOpen(open) {
    el.menuButton.setAttribute("aria-expanded", open ? "true" : "false");
    el.menuPanel.hidden = !open;
  }

  function api(path) {
    return fetch(path).then(function (res) {
      if (!res.ok) {
        return res.json().then(function (body) {
          throw new Error(body.error || res.statusText);
        });
      }
      return res.json();
    });
  }

  function debounce(fn, wait) {
    var timer = null;
    return function () {
      var args = arguments;
      clearTimeout(timer);
      timer = setTimeout(function () { fn.apply(null, args); }, wait);
    };
  }

  function loadSearch() {
    renderSearchFilter();
    var q = encodeURIComponent(currentSearchQuery());
    api("/api/search?limit=100&q=" + q).then(function (data) {
      state.results = data.results || [];
      renderResults();
      if (!state.selectedID) {
        renderHome();
      }
    }).catch(showError);
  }

  function currentSearchQuery() {
    var parts = state.activeTags.map(function (tag) { return "#" + tag; });
    var text = el.search.value.trim();
    if (text) parts.push(text);
    return parts.join(" ");
  }

  function renderSearchFilter() {
    el.searchChips.innerHTML = "";
    state.activeTags.forEach(function (tag) {
      var chip = document.createElement("button");
      chip.className = "search-chip";
      chip.type = "button";
      chip.dataset.tag = tag;
      chip.setAttribute("aria-label", "Remove tag filter #" + tag);
      chip.innerHTML = '<span aria-hidden="true">x</span><span>#' + escapeHTML(tag) + '</span>';
      el.searchChips.appendChild(chip);
    });
    el.search.placeholder = "Search notes";
  }

  function applyTagSearch(tag) {
    tag = String(tag || "").trim();
    if (!tag) return;
    if (!state.activeTags.some(function (active) { return active === tag; })) {
      state.activeTags.push(tag);
    }
    renderSearchFilter();
    loadSearch();
    el.search.focus();
  }

  function clearTagSearch(tag) {
    var next = state.activeTags.filter(function (active) { return active !== tag; });
    if (next.length === state.activeTags.length) return;
    state.activeTags = next;
    renderSearchFilter();
    loadSearch();
    el.search.focus();
  }

  function renderResults() {
    el.results.innerHTML = "";
    if (state.results.length === 0) {
      el.results.innerHTML = '<div class="empty">No notes found</div>';
      return;
    }
    state.results.forEach(function (note) {
      var button = document.createElement("button");
      button.className = "result" + (note.note_id === state.selectedID ? " active" : "");
      button.type = "button";
      button.dataset.noteId = note.note_id;
      button.onclick = function (event) {
        var tag = event.target.closest("[data-tag]");
        if (tag) {
          event.preventDefault();
          applyTagSearch(tag.dataset.tag);
          return;
        }
        selectNote(note.note_id, { history: "push" });
      };
      var badge = note.generated_by_ai ? '<span class="badge">generated</span>' : "";
      var tags = renderTags(note.tags || []);
      button.innerHTML = '<div class="result-title"><span>' + escapeHTML(note.title || "#" + note.note_id) + '</span>' + badge + '</div>' +
        tags +
        '<div class="result-meta">' + escapeHTML(note.file_kind || "note") + " / " + note.note_id + '</div>';
      el.results.appendChild(button);
    });
  }

  function renderTags(tags) {
    if (!tags || tags.length === 0) return "";
    return '<div class="tag-list">' + tags.map(function (tag) {
      return '<span class="tag" data-tag="' + escapeHTML(tag) + '">#' + escapeHTML(tag) + '</span>';
    }).join("") + '</div>';
  }

  function renderHome() {
    state.selectedID = null;
    renderResults();
    var notes = state.results.slice(0, 12);
    var body = '<h1>Recent Notes</h1><p class="home-summary">Showing ' + notes.length + ' recent notes</p>';
    if (notes.length === 0) {
      body += '<div class="empty">No notes found</div>';
    } else {
      body += '<div class="home-list">' + notes.map(function (note) {
        return '<a class="home-note" href="/?id=' + encodeURIComponent(note.note_id) + '" data-note-id="' + escapeHTML(note.note_id) + '">' +
          '<div class="home-note-title">' + escapeHTML(note.title || "#" + note.note_id) + '</div>' +
          renderTags(note.tags || []) +
          '</a>';
      }).join("") + '</div>';
    }
    el.body.innerHTML = body;
    el.backlinks.innerHTML = '<div class="empty">No backlinks</div>';
    setGraph({ nodes: [], edges: [] });
  }

  function goHome(mode) {
    hidePreview();
    state.selectedID = null;
    state.activeTags = [];
    el.search.value = "";
    renderSearchFilter();
    renderHome();
    updateHomeHistory(mode || "push");
    loadSearch();
    el.search.focus();
  }

  function selectNote(id, opts) {
    opts = opts || {};
    hidePreview();
    state.selectedID = id;
    renderResults();
    api("/api/note?id=" + encodeURIComponent(id)).then(function (data) {
      var note = data.note;
      el.body.innerHTML = renderMarkdown(note.body || "");
      insertNoteTags(note.tags || []);
      renderBacklinks(data.backlinks || []);
      updateHistory(note, opts.history || "push");
      return api("/api/graph/local?id=" + encodeURIComponent(id));
    }).then(function (data) {
      setGraph(data.graph || { nodes: [], edges: [] });
    }).catch(showError);
  }

  function insertNoteTags(tags) {
    var html = renderTags(tags);
    if (!html) return;
    html = html.replace('class="tag-list"', 'class="tag-list note-tags"');
    var firstTitle = el.body.querySelector("h1");
    if (firstTitle) {
      firstTitle.insertAdjacentHTML("afterend", html);
    } else {
      el.body.insertAdjacentHTML("afterbegin", html);
    }
  }

  function renderBacklinks(backlinks) {
    el.backlinks.innerHTML = "";
    if (backlinks.length === 0) {
      el.backlinks.innerHTML = '<div class="empty">No backlinks</div>';
      return;
    }
    backlinks.forEach(function (note) {
      var link = document.createElement("a");
      link.className = "backlink";
      link.href = "/?id=" + encodeURIComponent(note.note_id);
      link.dataset.noteId = note.note_id;
      link.textContent = note.title || "#" + note.note_id;
      link.onclick = function (event) {
        if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) return;
        event.preventDefault();
        selectNote(note.note_id, { history: "push" });
      };
      el.backlinks.appendChild(link);
    });
  }

  function updateHistory(note, mode) {
    if (mode === "none") return;
    var url = new URL(window.location.href);
    url.searchParams.set("id", note.note_id);
    var payload = { note_id: note.note_id };
    if (mode === "replace") {
      window.history.replaceState(payload, "", url);
    } else {
      if (window.history.state && window.history.state.note_id === note.note_id) return;
      window.history.pushState(payload, "", url);
    }
  }

  function updateHomeHistory(mode) {
    if (mode === "none") return;
    var url = new URL(window.location.href);
    url.searchParams.delete("id");
    if (mode === "replace") {
      window.history.replaceState({ home: true }, "", url);
    } else {
      window.history.pushState({ home: true }, "", url);
    }
  }

  function openLinkTerm(term) {
    var target = normalizeTerm(term);
    if (!target) return;
    api("/api/resolve?term=" + encodeURIComponent(target)).then(function (data) {
      if (data.found && data.note && data.note.note_id) {
        selectNote(data.note.note_id, { history: "push" });
      } else {
        markUnresolved(target);
      }
    }).catch(showError);
  }

  function normalizeTerm(term) {
    return String(term || "").split("#")[0].trim();
  }

  function markUnresolved(term) {
    var buttons = el.body.querySelectorAll('.wiki-link[data-term="' + cssEscape(term) + '"]');
    buttons.forEach(function (button) {
      button.classList.add("unresolved");
      button.title = "Unresolved: " + term;
    });
  }

  function renderMarkdown(markdown) {
    var lines = markdown.split("\n");
    var html = [];
    var inCode = false;
    var para = [];
    function flushPara() {
      if (para.length) {
        html.push("<p>" + inline(para.join(" ")) + "</p>");
        para = [];
      }
    }
    lines.forEach(function (line) {
      if (line.indexOf(String.fromCharCode(96, 96, 96)) === 0) {
        if (inCode) {
          html.push("</code></pre>");
          inCode = false;
        } else {
          flushPara();
          html.push("<pre><code>");
          inCode = true;
        }
        return;
      }
      if (inCode) {
        html.push(escapeHTML(line) + "\n");
        return;
      }
      if (line.trim() === "") {
        flushPara();
        return;
      }
      var h = line.match(/^(#{1,3})\s+(.*)$/);
      if (h) {
        flushPara();
        html.push("<h" + h[1].length + ">" + inline(h[2]) + "</h" + h[1].length + ">");
        return;
      }
      para.push(line.trim());
    });
    flushPara();
    if (inCode) {
      html.push("</code></pre>");
    }
    return html.join("");
  }

  function inline(text) {
    var escaped = escapeHTML(text);
    return escaped
      .replace(/\[\[([^\]|]+)\|([^\]]+)\]\]/g, function (_, target, display) {
        return wikiButton(target, display);
      })
      .replace(/\[\[([^\]]+)\]\]/g, function (_, target) {
        return wikiButton(target, target);
      });
  }

  function wikiButton(target, display) {
    var term = normalizeTerm(unescapeHTML(target));
    return '<button type="button" class="wiki-link" data-term="' + escapeHTML(term) + '">' + display + '</button>';
  }

  function escapeHTML(value) {
    return String(value).replace(/[&<>"']/g, function (ch) {
      return ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[ch];
    });
  }

  function unescapeHTML(value) {
    var textarea = document.createElement("textarea");
    textarea.innerHTML = value;
    return textarea.value;
  }

  function cssEscape(value) {
    if (window.CSS && window.CSS.escape) return window.CSS.escape(value);
    return String(value).replace(/["\\]/g, "\\$&");
  }

  function previewTarget(node) {
    if (node && node.closest && node.closest("[data-tag]")) return null;
    var target = node && node.closest ? node.closest(".wiki-link, .backlink, .home-note, .result") : null;
    if (!target) return null;
    return target;
  }

  function ensurePreview() {
    if (state.preview.panel) return state.preview.panel;
    var panel = document.createElement("div");
    panel.className = "note-preview";
    panel.hidden = true;
    panel.addEventListener("mouseenter", clearPreviewHide);
    panel.addEventListener("mouseleave", schedulePreviewHide);
    document.body.appendChild(panel);
    state.preview.panel = panel;
    return panel;
  }

  function clearPreviewHide() {
    clearTimeout(state.preview.hideTimer);
    state.preview.hideTimer = null;
  }

  function schedulePreviewHide() {
    clearPreviewHide();
    state.preview.hideTimer = setTimeout(hidePreview, 140);
  }

  function hidePreview() {
    clearPreviewHide();
    if (state.preview.panel) state.preview.panel.hidden = true;
  }

  function showPreviewFor(target) {
    clearPreviewHide();
    var panel = ensurePreview();
    var request = ++state.preview.request;
    panel.hidden = false;
    panel.innerHTML = '<div class="empty">Loading preview</div>';
    positionPreview(panel, target);
    previewData(target).then(function (data) {
      if (request !== state.preview.request) return;
      panel.innerHTML = data.html;
      positionPreview(panel, target);
    }).catch(function () {
      if (request !== state.preview.request) return;
      panel.innerHTML = '<div class="empty">Preview unavailable</div>';
      positionPreview(panel, target);
    });
  }

  function previewData(target) {
    if (target.classList.contains("wiki-link")) {
      var term = normalizeTerm(target.dataset.term);
      if (!term) return Promise.reject(new Error("empty link"));
      return api("/api/resolve?term=" + encodeURIComponent(term)).then(function (data) {
        if (!data.found || !data.note || !data.note.note_id) {
          return { html: '<div class="empty">Unresolved: ' + escapeHTML(term) + '</div>' };
        }
        return previewNote(data.note.note_id);
      });
    }
    var id = Number(target.dataset.noteId);
    if (!id) return Promise.reject(new Error("missing note id"));
    return previewNote(id);
  }

  function previewNote(id) {
    if (state.preview.cache[id]) {
      return Promise.resolve(state.preview.cache[id]);
    }
    return api("/api/note?id=" + encodeURIComponent(id)).then(function (data) {
      var note = data.note || {};
      var html = '<div class="note-preview-title">' + escapeHTML(note.title || "#" + note.note_id) + '</div>' +
        renderTags(note.tags || []) +
        '<div class="note-preview-body">' + renderMarkdown(previewMarkdown(note.body || "")) + '</div>';
      var result = { html: html };
      state.preview.cache[id] = result;
      return result;
    });
  }

  function previewMarkdown(markdown) {
    var lines = markdown.split("\n");
    var out = [];
    var chars = 0;
    for (var i = 0; i < lines.length; i++) {
      if (i === 0 && /^#\s+/.test(lines[i])) continue;
      out.push(lines[i]);
      chars += lines[i].length;
      if (out.length >= 12 || chars > 900) break;
    }
    return out.join("\n");
  }

  function positionPreview(panel, target) {
    var rect = target.getBoundingClientRect();
    var margin = 12;
    var panelRect = panel.getBoundingClientRect();
    var width = panelRect.width || 380;
    var height = panelRect.height || 260;
    var left = rect.right + margin;
    if (left + width > window.innerWidth - margin) {
      left = rect.left - width - margin;
    }
    if (left < margin) {
      left = Math.min(Math.max(margin, rect.left), window.innerWidth - width - margin);
    }
    var top = rect.top;
    if (top + height > window.innerHeight - margin) {
      top = window.innerHeight - height - margin;
    }
    panel.style.left = Math.max(margin, left) + "px";
    panel.style.top = Math.max(margin, top) + "px";
  }

  function showError(err) {
    console.error(err);
    el.body.innerHTML = '<div class="empty">' + escapeHTML(err.message || String(err)) + '</div>';
  }

  function setGraph(graph) {
    state.graph = graph;
    state.nodes = (graph.nodes || []).map(function (node, i) {
      var angle = (Math.PI * 2 * i) / Math.max(1, graph.nodes.length);
      return Object.assign({}, node, {
        x: Math.cos(angle) * 160,
        y: Math.sin(angle) * 160,
        vx: 0,
        vy: 0
      });
    });
    var byID = {};
    state.nodes.forEach(function (node) { byID[node.note_id] = node; });
    state.edges = (graph.edges || []).map(function (edge) {
      return { source: byID[edge.source_id], target: byID[edge.target_id] };
    }).filter(function (edge) { return edge.source && edge.target; });
    resizeCanvas();
    state.graphView = fitGraphView();
    el.graphMeta.textContent = state.nodes.length + " nodes / " + state.edges.length + " links";
    startGraph();
  }

  function visibleNodes() {
    return state.nodes;
  }

  function startGraph() {
    if (state.animation) cancelAnimationFrame(state.animation);
    resizeCanvas();
    var ticks = 0;
    function frame() {
      stepGraph();
      drawGraph();
      ticks++;
      state.animation = requestAnimationFrame(frame);
    }
    frame();
  }

  function resizeCanvas() {
    var ratio = window.devicePixelRatio || 1;
    var rect = el.canvas.getBoundingClientRect();
    el.canvas.width = Math.max(1, Math.floor(rect.width * ratio));
    el.canvas.height = Math.max(1, Math.floor(rect.height * ratio));
  }

  function fitGraphView() {
    if (state.nodes.length === 0) {
      return { x: 0, y: 0, scale: 1 };
    }
    var minX = Infinity;
    var maxX = -Infinity;
    var minY = Infinity;
    var maxY = -Infinity;
    state.nodes.forEach(function (node) {
      minX = Math.min(minX, node.x);
      maxX = Math.max(maxX, node.x);
      minY = Math.min(minY, node.y);
      maxY = Math.max(maxY, node.y);
    });
    var padding = 96 * (window.devicePixelRatio || 1);
    var graphW = Math.max(1, maxX - minX);
    var graphH = Math.max(1, maxY - minY);
    var availW = Math.max(1, el.canvas.width - padding);
    var availH = Math.max(1, el.canvas.height - padding);
    var scale = Math.max(0.05, Math.min(1.5, Math.min(availW / graphW, availH / graphH)));
    var centerX = (minX + maxX) / 2;
    var centerY = (minY + maxY) / 2;
    return {
      x: -centerX * scale,
      y: -centerY * scale,
      scale: scale
    };
  }

  function stepGraph() {
    var nodes = visibleNodes();
    var visible = {};
    nodes.forEach(function (node) { visible[node.note_id] = true; });
    for (var i = 0; i < nodes.length; i++) {
      for (var j = i + 1; j < nodes.length; j++) {
        var a = nodes[i], b = nodes[j];
        var dx = a.x - b.x;
        var dy = a.y - b.y;
        var d2 = Math.max(80, dx * dx + dy * dy);
        var f = 1400 / d2;
        a.vx += dx * f;
        a.vy += dy * f;
        b.vx -= dx * f;
        b.vy -= dy * f;
      }
    }
    state.edges.forEach(function (edge) {
      if (!visible[edge.source.note_id] || !visible[edge.target.note_id]) return;
      var dx = edge.target.x - edge.source.x;
      var dy = edge.target.y - edge.source.y;
      var dist = Math.max(1, Math.sqrt(dx * dx + dy * dy));
      var force = (dist - 110) * 0.012;
      var fx = dx / dist * force;
      var fy = dy / dist * force;
      edge.source.vx += fx;
      edge.source.vy += fy;
      edge.target.vx -= fx;
      edge.target.vy -= fy;
    });
    nodes.forEach(function (node) {
      node.vx += -node.x * 0.002;
      node.vy += -node.y * 0.002;
      node.vx *= 0.82;
      node.vy *= 0.82;
      node.x += node.vx;
      node.y += node.vy;
    });
  }

  function drawGraph() {
    var canvas = el.canvas;
    var ctx = canvas.getContext("2d");
    var ratio = window.devicePixelRatio || 1;
    var w = canvas.width;
    var h = canvas.height;
    ctx.clearRect(0, 0, w, h);
    ctx.save();
    ctx.translate(w / 2 + state.graphView.x, h / 2 + state.graphView.y);
    ctx.scale(state.graphView.scale, state.graphView.scale);
    ctx.font = Math.floor(12 * ratio / state.graphView.scale) + "px system-ui, sans-serif";
    ctx.lineWidth = 1 * ratio / state.graphView.scale;
    var visible = {};
    visibleNodes().forEach(function (node) { visible[node.note_id] = true; });
    ctx.globalAlpha = 0.62;
    ctx.strokeStyle = css("--line");
    state.edges.forEach(function (edge) {
      if (!visible[edge.source.note_id] || !visible[edge.target.note_id]) return;
      ctx.beginPath();
      ctx.moveTo(edge.source.x, edge.source.y);
      ctx.lineTo(edge.target.x, edge.target.y);
      ctx.stroke();
    });
    ctx.globalAlpha = 0.9;
    visibleNodes().forEach(function (node) {
      var radius = node.center ? 10 * ratio / state.graphView.scale : 7 * ratio / state.graphView.scale;
      ctx.beginPath();
      ctx.arc(node.x, node.y, radius, 0, Math.PI * 2);
      ctx.fillStyle = node.center ? css("--accent") : (node.generated_by_ai ? css("--generated") : css("--panel-soft"));
      ctx.strokeStyle = node.center ? css("--accent-strong") : css("--muted");
      ctx.fill();
      ctx.stroke();
      ctx.globalAlpha = node.center ? 0.95 : 0.78;
      ctx.fillStyle = css("--text");
      ctx.fillText(trim(node.title || "#" + node.note_id, 20), node.x + radius + 5, node.y + 4 * ratio / state.graphView.scale);
      ctx.globalAlpha = 0.9;
    });
    ctx.restore();
  }

  function canvasPoint(event) {
    var rect = el.canvas.getBoundingClientRect();
    var ratio = window.devicePixelRatio || 1;
    return {
      x: (event.clientX - rect.left) * ratio,
      y: (event.clientY - rect.top) * ratio
    };
  }

  function worldPoint(point) {
    return {
      x: (point.x - el.canvas.width / 2 - state.graphView.x) / state.graphView.scale,
      y: (point.y - el.canvas.height / 2 - state.graphView.y) / state.graphView.scale
    };
  }

  function graphNodeAt(point) {
    var world = worldPoint(point);
    var best = null;
    var bestD = Infinity;
    visibleNodes().forEach(function (node) {
      var dx = node.x - world.x;
      var dy = node.y - world.y;
      var d = dx * dx + dy * dy;
      if (d < bestD) {
        bestD = d;
        best = node;
      }
    });
    var threshold = 34 * (window.devicePixelRatio || 1) / state.graphView.scale;
    return best && bestD <= threshold * threshold ? best : null;
  }

  function activePointerPoints() {
    return Object.keys(state.pointers).map(function (id) {
      return state.pointers[id];
    });
  }

  function midpoint(a, b) {
    return {
      x: (a.x + b.x) / 2,
      y: (a.y + b.y) / 2
    };
  }

  function distance(a, b) {
    var dx = a.x - b.x;
    var dy = a.y - b.y;
    return Math.max(1, Math.sqrt(dx * dx + dy * dy));
  }

  function startPinch(a, b) {
    var center = midpoint(a, b);
    return {
      distance: distance(a, b),
      center: center,
      world: worldPoint(center),
      scale: state.graphView.scale,
      moved: false
    };
  }

  function updatePinch(a, b) {
    if (!state.pinch) {
      state.pinch = startPinch(a, b);
      return;
    }
    var center = midpoint(a, b);
    var nextScale = Math.max(0.05, Math.min(4, state.pinch.scale * distance(a, b) / state.pinch.distance));
    state.graphView.scale = nextScale;
    state.graphView.x = center.x - el.canvas.width / 2 - state.pinch.world.x * nextScale;
    state.graphView.y = center.y - el.canvas.height / 2 - state.pinch.world.y * nextScale;
    if (Math.abs(center.x - state.pinch.center.x) + Math.abs(center.y - state.pinch.center.y) > 4 || Math.abs(nextScale - state.pinch.scale) > 0.02) {
      state.pinch.moved = true;
    }
  }

  function css(name) {
    return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  }

  function trim(text, max) {
    text = String(text);
    return text.length > max ? text.slice(0, max - 1) + "..." : text;
  }

  el.canvas.addEventListener("pointerdown", function (event) {
    event.preventDefault();
    var point = canvasPoint(event);
    state.pointers[event.pointerId] = point;
    el.canvas.setPointerCapture(event.pointerId);
    el.canvas.classList.add("dragging");
    var points = activePointerPoints();
    if (points.length >= 2) {
      state.dragging = null;
      state.pinch = startPinch(points[0], points[1]);
      return;
    }
    state.dragging = {
      pointerId: event.pointerId,
      start: point,
      last: point,
      moved: false
    };
  });

  el.canvas.addEventListener("pointermove", function (event) {
    if (!state.pointers[event.pointerId]) return;
    event.preventDefault();
    state.pointers[event.pointerId] = canvasPoint(event);
    var points = activePointerPoints();
    if (points.length >= 2) {
      updatePinch(points[0], points[1]);
      drawGraph();
      return;
    }
    if (!state.dragging || state.dragging.pointerId !== event.pointerId) return;
    var point = state.pointers[event.pointerId];
    var dx = point.x - state.dragging.last.x;
    var dy = point.y - state.dragging.last.y;
    state.graphView.x += dx;
    state.graphView.y += dy;
    if (Math.abs(point.x - state.dragging.start.x) + Math.abs(point.y - state.dragging.start.y) > 4) {
      state.dragging.moved = true;
    }
    state.dragging.last = point;
    drawGraph();
  });

  el.canvas.addEventListener("pointerup", function (event) {
    var point = state.pointers[event.pointerId] || canvasPoint(event);
    delete state.pointers[event.pointerId];
    var points = activePointerPoints();
    if (state.pinch) {
      state.pinch = null;
      state.dragging = null;
      if (points.length === 1) {
        state.dragging = {
          pointerId: Number(Object.keys(state.pointers)[0]),
          start: points[0],
          last: points[0],
          moved: true
        };
      } else {
        el.canvas.classList.remove("dragging");
      }
      return;
    }
    if (!state.dragging || state.dragging.pointerId !== event.pointerId) return;
    var moved = state.dragging.moved;
    state.dragging = null;
    if (points.length === 0) {
      el.canvas.classList.remove("dragging");
    }
    if (!moved) {
      var node = graphNodeAt(point);
      if (node) selectNote(node.note_id, { history: "push" });
    }
  });

  el.canvas.addEventListener("pointercancel", function (event) {
    delete state.pointers[event.pointerId];
    state.dragging = null;
    state.pinch = null;
    el.canvas.classList.remove("dragging");
  });

  el.canvas.addEventListener("wheel", function (event) {
    event.preventDefault();
    var point = canvasPoint(event);
    var before = worldPoint(point);
    var factor = Math.exp(-event.deltaY * 0.001);
    state.graphView.scale = Math.max(0.05, Math.min(4, state.graphView.scale * factor));
    state.graphView.x = point.x - el.canvas.width / 2 - before.x * state.graphView.scale;
    state.graphView.y = point.y - el.canvas.height / 2 - before.y * state.graphView.scale;
    drawGraph();
  }, { passive: false });

  document.addEventListener("mouseover", function (event) {
    var target = previewTarget(event.target);
    if (!target) return;
    if (target.contains(event.relatedTarget)) return;
    showPreviewFor(target);
  });

  document.addEventListener("mouseout", function (event) {
    var target = previewTarget(event.target);
    if (!target) return;
    var panel = state.preview.panel;
    if (target.contains(event.relatedTarget) || (panel && panel.contains(event.relatedTarget))) return;
    schedulePreviewHide();
  });

  document.addEventListener("click", function (event) {
    var panel = state.preview.panel;
    if (!panel || !panel.contains(event.target)) return;
    var tag = event.target.closest("[data-tag]");
    if (tag) {
      event.preventDefault();
      applyTagSearch(tag.dataset.tag);
      return;
    }
    var link = event.target.closest(".wiki-link");
    if (link) {
      event.preventDefault();
      openLinkTerm(link.dataset.term);
    }
  });

  el.body.addEventListener("click", function (event) {
    var tag = event.target.closest("[data-tag]");
    if (tag) {
      event.preventDefault();
      applyTagSearch(tag.dataset.tag);
      return;
    }
    var noteLink = event.target.closest(".home-note");
    if (noteLink) {
      if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) return;
      event.preventDefault();
      selectNote(Number(noteLink.dataset.noteId), { history: "push" });
      return;
    }
    var link = event.target.closest(".wiki-link");
    if (!link) return;
    event.preventDefault();
    openLinkTerm(link.dataset.term);
  });

  window.addEventListener("popstate", function (event) {
    var id = event.state && event.state.note_id;
    if (!id) {
      id = Number(new URL(window.location.href).searchParams.get("id"));
    }
    if (id) {
      selectNote(id, { history: "none" });
    } else {
      goHome("none");
    }
  });

  window.addEventListener("resize", function () {
    resizeCanvas();
    drawGraph();
  });
  el.menuButton.addEventListener("click", function (event) {
    event.stopPropagation();
    setMenuOpen(el.menuButton.getAttribute("aria-expanded") !== "true");
  });
  el.menuPanel.addEventListener("click", function (event) {
    event.stopPropagation();
  });
  document.addEventListener("click", function () {
    setMenuOpen(false);
  });
  document.addEventListener("keydown", function (event) {
    if (event.key === "Escape") setMenuOpen(false);
  });
  el.themeButtons.forEach(function (button) {
    button.addEventListener("click", function () {
      applyTheme(button.dataset.themeChoice);
      setMenuOpen(false);
    });
  });
  if (systemTheme) {
    systemTheme.addEventListener("change", function () {
      if (themeMode() === "system") drawGraph();
    });
  }
  el.searchChips.addEventListener("click", function (event) {
    var chip = event.target.closest(".search-chip");
    if (!chip) return;
    clearTagSearch(chip.dataset.tag);
  });
  var debouncedLoadSearch = debounce(loadSearch, 160);
  el.search.addEventListener("input", debouncedLoadSearch);
  el.homeLink.addEventListener("click", function (event) {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) return;
    event.preventDefault();
    goHome("push");
  });

  applyTheme(themeMode());
  var initialID = Number(new URL(window.location.href).searchParams.get("id"));
  if (initialID) {
    window.history.replaceState({ note_id: initialID }, "", window.location.href);
    selectNote(initialID, { history: "none" });
  } else {
    window.history.replaceState({ home: true }, "", window.location.href);
  }
  loadSearch();
})();
`
