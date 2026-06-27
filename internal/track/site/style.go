package site

// styleCSS is the stylesheet shipped alongside the generated pages. It is intentionally small and
// dependency-free: readable typography, a centered measure, and basic code/table styling so the site
// looks reasonable on GitHub Pages without a build step.
const styleCSS = `:root {
  color-scheme: light dark;
  --fg: #1f2328;
  --bg: #ffffff;
  --muted: #57606a;
  --border: #d0d7de;
  --accent: #e8920a;
  --code-bg: #f6f8fa;
}
@media (prefers-color-scheme: dark) {
  :root {
    --fg: #e6edf3;
    --bg: #0d1117;
    --muted: #8b949e;
    --border: #30363d;
    --code-bg: #161b22;
  }
}
* { box-sizing: border-box; }
body {
  margin: 0;
  background: var(--bg);
  color: var(--fg);
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Hiragino Sans", "Noto Sans JP", sans-serif;
  line-height: 1.7;
  word-break: normal;
  overflow-wrap: anywhere;
}
.note {
  max-width: 760px;
  margin: 0 auto;
  padding: 2.5rem 1.25rem 5rem;
}
.site-nav { margin-bottom: 1.5rem; font-size: 0.9rem; }
.site-nav a { color: var(--muted); text-decoration: none; }
.site-nav a:hover { color: var(--accent); }
article a { color: var(--accent); text-decoration: none; }
article a:hover { text-decoration: underline; }
h1, h2, h3, h4 { line-height: 1.3; margin-top: 2rem; }
h1 { font-size: 1.9rem; }
h2 { border-bottom: 1px solid var(--border); padding-bottom: 0.3rem; }
code {
  background: var(--code-bg);
  padding: 0.15em 0.35em;
  border-radius: 4px;
  font-size: 0.9em;
}
pre {
  background: var(--code-bg);
  padding: 1rem;
  border-radius: 6px;
  overflow-x: auto;
}
pre code { background: none; padding: 0; }
blockquote {
  margin: 1rem 0;
  padding: 0 1rem;
  color: var(--muted);
  border-left: 3px solid var(--border);
}
table { border-collapse: collapse; width: 100%; margin: 1rem 0; }
th, td { border: 1px solid var(--border); padding: 0.4rem 0.7rem; }
th { background: var(--code-bg); }
img { max-width: 100%; height: auto; }
ul.contains-task-list { list-style: none; padding-left: 1rem; }
`
