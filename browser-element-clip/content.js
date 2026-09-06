// Injected element-picker content script (MV3). Activation is driven by the
// background service worker: it injects this file and then sends
// {type: "track-elem:start"}. The picker highlights the hovered element, locks
// the payload on click, and cancels on Escape. All UI lives in Shadow DOM so it
// cannot collide with page CSS. The script only captures — track-fetch-elem is
// the converter (see docs/spec/fetch.md, "Web element clip").
(() => {
  "use strict";

  if (window.__trackElemClipLoaded) return;
  window.__trackElemClipLoaded = true;

  // Allowlisted attribute names mirror the converter (plus aria-*).
  const SAFE_ATTRS = new Set([
    "id", "class", "name", "type", "role", "href", "src", "alt", "title",
    "placeholder", "for", "action", "method",
  ]);

  // Curated computed-style subset, camelCase keys matching the converter.
  const STYLE_PROPS = [
    "display", "position", "width", "height", "margin", "padding", "color",
    "backgroundColor", "border", "borderRadius", "fontFamily", "fontSize",
    "fontWeight", "lineHeight", "textAlign", "zIndex",
  ];

  // Budget caps mirrored from the converter's DefaultBudget.
  const BUDGET = {
    textSnippet: 200,
    htmlSnippet: 4096,
    selector: 700,
    path: 900,
    metadata: 500,
    nearbyTextEntries: 10,
    nearbyTextLength: 200,
    nearbyElements: 6,
    nearbyElementLength: 160,
    ancestorPath: 10,
    selectedText: 500,
  };

  const state = {
    active: false,
    hovered: null,
    overlayHost: null,
    overlay: null,
    label: null,
  };

  function collapse(s) {
    return String(s == null ? "" : s).replace(/\s+/g, " ").trim();
  }

  function truncate(s, max) {
    const str = String(s == null ? "" : s);
    if (str.length <= max) return str;
    return str.slice(0, max);
  }

  // ---- selector / path builders -------------------------------------------

  function nthIndex(el) {
    if (!el.parentElement) return null;
    const tag = el.tagName.toLowerCase();
    let n = 0;
    let i = 0;
    for (const child of el.parentElement.children) {
      if (child.tagName.toLowerCase() === tag) {
        n += 1;
        if (child === el) {
          i = n;
          break;
        }
      }
    }
    return i > 0 ? i : null;
  }

  function segment(el) {
    if (!el || el.nodeType !== 1) return "";
    const tag = el.tagName.toLowerCase();
    if (el.id) return `${tag}#${el.id}`;
    return tag;
  }

  // CSS selector from the element up to the nearest id (or document root).
  function buildSelector(el) {
    const parts = [];
    let cur = el;
    while (cur && cur.nodeType === 1) {
      let seg = segment(cur);
      if (cur !== el && cur.id) {
        seg = segment(cur);
        parts.push(seg);
        break;
      }
      if (!cur.id) {
        const idx = nthIndex(cur);
        if (idx != null) seg += `:nth-of-type(${idx})`;
      }
      parts.push(seg);
      cur = cur.parentElement;
    }
    parts.reverse();
    return truncate(parts.join(" > "), BUDGET.selector);
  }

  // Compact tag:nth-child path (DevTools-style).
  function buildElementPath(el) {
    const parts = [];
    let cur = el;
    while (cur && cur.nodeType === 1) {
      let seg = cur.tagName.toLowerCase();
      const idx = nthIndex(cur);
      if (idx != null) seg += `:nth-child(${idx})`;
      parts.push(seg);
      cur = cur.parentElement;
    }
    parts.reverse();
    return truncate(parts.join(" > "), BUDGET.path);
  }

  // Full path using ids/classes where available.
  function buildFullPath(el) {
    const parts = [];
    let cur = el;
    while (cur && cur.nodeType === 1) {
      let seg = cur.tagName.toLowerCase();
      if (cur.id) seg += `#${cur.id}`;
      else if (cur.classList && cur.classList.length) {
        seg += "." + Array.from(cur.classList).join(".");
      }
      parts.push(seg);
      cur = cur.parentElement;
    }
    parts.reverse();
    return truncate(parts.join(" > "), BUDGET.path);
  }

  // ---- capture helpers -----------------------------------------------------

  function collectAttributes(el) {
    const out = {};
    for (const attr of Array.from(el.attributes || [])) {
      const name = attr.name.toLowerCase();
      if (!SAFE_ATTRS.has(name) && !name.startsWith("aria-")) continue;
      out[name] = truncate(attr.value, 2000);
    }
    return out;
  }

  const IMPLICIT_ROLES = {
    a: "link", button: "button", img: "img", input: "textbox",
    textarea: "textbox", select: "combobox", h1: "heading", h2: "heading",
    h3: "heading", h4: "heading", h5: "heading", h6: "heading",
    nav: "navigation", main: "main", form: "form", table: "table",
    ul: "list", ol: "list", li: "listitem", article: "article",
    aside: "complementary", footer: "contentinfo", header: "banner",
  };

  function collectAccessibility(el) {
    const tag = el.tagName.toLowerCase();
    const ariaLabel = el.getAttribute("aria-label") || "";
    const role = el.getAttribute("role") || IMPLICIT_ROLES[tag] || "";
    const accessibleName =
      ariaLabel ||
      el.getAttribute("title") ||
      el.getAttribute("alt") ||
      collapse(el.innerText).slice(0, 200) ||
      "";
    return { role, accessibleName, ariaLabel };
  }

  function isNoise(prop, value) {
    if (value === "" || value === "auto" || value === "normal") return true;
    if (prop === "position" && value === "static") return true;
    if (prop === "display" && value === "inline") return true;
    if (prop === "backgroundColor" && value === "rgba(0, 0, 0, 0)") return true;
    return false;
  }

  function collectComputedStyles(el) {
    const cs = getComputedStyle(el);
    const out = {};
    for (const prop of STYLE_PROPS) {
      let value = cs[prop];
      if (typeof value !== "string") value = "";
      if (!isNoise(prop, value)) out[prop] = value;
    }
    return out;
  }

  function collectAncestorPath(el) {
    const parts = [];
    let cur = el.parentElement;
    while (cur && cur.nodeType === 1) {
      parts.push(cur.tagName.toLowerCase());
      cur = cur.parentElement;
    }
    parts.reverse();
    if (parts.length > BUDGET.ancestorPath) {
      parts.splice(0, parts.length - BUDGET.ancestorPath);
    }
    return parts;
  }

  function collectNearbyText(el) {
    const seen = new Set();
    const out = [];
    const target = collapse(el.innerText);
    const push = (node) => {
      if (!node || node.nodeType !== 1) return;
      const text = collapse(node.innerText);
      if (!text || text === target || seen.has(text)) return;
      if (out.length >= BUDGET.nearbyTextEntries) return;
      seen.add(text);
      out.push(truncate(text, BUDGET.nearbyTextLength));
    };
    push(el.parentElement);
    if (el.parentElement) {
      for (const sib of el.parentElement.children) push(sib);
    }
    return out;
  }

  function collectNearbyElements(el) {
    const out = [];
    if (!el.parentElement) return out;
    for (const sib of el.parentElement.children) {
      if (sib === el || out.length >= BUDGET.nearbyElements) continue;
      let desc = sib.tagName.toLowerCase();
      if (sib.classList && sib.classList.length) desc += "." + Array.from(sib.classList).join(".");
      const text = collapse(sib.innerText);
      if (text) desc += ` "${truncate(text, 80)}"`;
      out.push(truncate(desc, BUDGET.nearbyElementLength));
    }
    return out;
  }

  function sanitizeUrl(raw) {
    try {
      const u = new URL(raw, location.href);
      if (u.protocol !== "http:" && u.protocol !== "https:") return "";
      u.search = "";
      u.hash = "";
      return u.toString();
    } catch {
      return "";
    }
  }

  function capture(el) {
    const rect = el.getBoundingClientRect();
    const sel = getSelection();
    return {
      page: {
        sanitizedUrl: sanitizeUrl(location.href),
        title: document.title || "",
        viewportWidth: window.innerWidth || document.documentElement.clientWidth,
        viewportHeight: window.innerHeight || document.documentElement.clientHeight,
        capturedAt: new Date().toISOString(),
      },
      target: {
        tagName: el.tagName.toLowerCase(),
        selector: buildSelector(el),
        elementPath: buildElementPath(el),
        fullPath: buildFullPath(el),
        cssClasses: collapse(el.className && el.className.baseVal != null
          ? el.className.baseVal : el.className),
        selectedText: collapse(sel && sel.toString ? sel.toString() : "").slice(0, BUDGET.selectedText),
        textSnippet: collapse(el.innerText).slice(0, BUDGET.textSnippet),
        htmlSnippet: truncate(el.outerHTML, BUDGET.htmlSnippet),
        attributes: collectAttributes(el),
        accessibility: collectAccessibility(el),
        rectViewport: {
          x: Math.round(rect.x),
          y: Math.round(rect.y),
          width: Math.round(rect.width),
          height: Math.round(rect.height),
        },
        computedStyles: collectComputedStyles(el),
      },
      nearbyText: collectNearbyText(el),
      ancestorPath: collectAncestorPath(el),
      nearbyElements: collectNearbyElements(el),
    };
  }

  // ---- clipboard -----------------------------------------------------------

  async function copyText(text) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      const ta = document.createElement("textarea");
      ta.value = text;
      ta.style.position = "fixed";
      ta.style.left = "-9999px";
      document.body.appendChild(ta);
      ta.select();
      let ok = false;
      try {
        ok = document.execCommand("copy");
      } catch {
        ok = false;
      }
      ta.remove();
      return ok;
    }
  }

  // ---- picker UI (Shadow DOM) ----------------------------------------------

  function buildStyles() {
    const style = document.createElement("style");
    style.textContent = `
      :host { all: initial; }
      .overlay {
        position: fixed;
        pointer-events: none;
        border: 2px solid #d94f70;
        background: rgba(217, 79, 112, 0.12);
        border-radius: 2px;
        box-sizing: border-box;
        z-index: 2147483647;
        transition: all 40ms ease-out;
      }
      .label {
        position: fixed;
        pointer-events: none;
        max-width: 60vw;
        padding: 3px 7px;
        background: #d94f70;
        color: #fff;
        font: 11px/1.4 -apple-system, "Segoe UI", sans-serif;
        border-radius: 3px;
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
        z-index: 2147483647;
      }
      .toast {
        position: fixed;
        left: 50%;
        bottom: 24px;
        transform: translateX(-50%);
        padding: 8px 14px;
        background: #222;
        color: #fff;
        font: 13px/1.4 -apple-system, "Segoe UI", sans-serif;
        border-radius: 6px;
        box-shadow: 0 4px 16px rgba(0,0,0,0.25);
        z-index: 2147483647;
      }
    `;
    return style;
  }

  function showToast(text) {
    const host = document.createElement("div");
    const root = host.attachShadow({ mode: "closed" });
    root.appendChild(buildStyles());
    const toast = document.createElement("div");
    toast.className = "toast";
    toast.textContent = text;
    root.appendChild(toast);
    document.documentElement.appendChild(host);
    setTimeout(() => host.remove(), 2600);
  }

  function positionOverlay(el) {
    const rect = el.getBoundingClientRect();
    state.overlay.style.left = rect.left + "px";
    state.overlay.style.top = rect.top + "px";
    state.overlay.style.width = rect.width + "px";
    state.overlay.style.height = rect.height + "px";
    state.label.textContent = segment(el);
    const labelRect = state.label.getBoundingClientRect();
    let top = rect.top - labelRect.height - 6;
    if (top < 0) top = rect.top + rect.height + 6;
    state.label.style.left = Math.min(rect.left, window.innerWidth - labelRect.width - 8) + "px";
    state.label.style.top = top + "px";
  }

  function mountOverlay() {
    const host = document.createElement("div");
    host.style.all = "initial";
    const root = host.attachShadow({ mode: "closed" });
    root.appendChild(buildStyles());
    const overlay = document.createElement("div");
    overlay.className = "overlay";
    const label = document.createElement("div");
    label.className = "label";
    root.appendChild(overlay);
    root.appendChild(label);
    document.documentElement.appendChild(host);
    state.overlayHost = host;
    state.overlay = overlay;
    state.label = label;
  }

  function teardown() {
    state.active = false;
    state.hovered = null;
    if (state.overlayHost) {
      state.overlayHost.remove();
      state.overlayHost = null;
      state.overlay = null;
      state.label = null;
    }
    document.removeEventListener("mousemove", onMouseMove, true);
    document.removeEventListener("click", onClick, true);
    document.removeEventListener("keydown", onKeyDown, true);
    document.documentElement.style.cursor = "";
  }

  function onMouseMove(ev) {
    if (!state.active) return;
    const el = ev.target;
    if (!el || el.nodeType !== 1) return;
    if (state.overlayHost && el === state.overlayHost) return;
    state.hovered = el;
    positionOverlay(el);
  }

  function onClick(ev) {
    if (!state.active) return;
    ev.preventDefault();
    ev.stopPropagation();
    ev.stopImmediatePropagation();
    const el = ev.target;
    if (!el || el.nodeType !== 1) return;
    teardown();
    const payload = capture(el);
    const json = JSON.stringify(payload, null, 2);
    copyText(json).then((ok) => {
      showToast(ok
        ? `Copied <${payload.target.tagName}> payload to clipboard`
        : "Failed to copy to clipboard");
    });
  }

  function onKeyDown(ev) {
    if (!state.active) return;
    if (ev.key === "Escape") {
      ev.preventDefault();
      ev.stopPropagation();
      teardown();
      showToast("Selection cancelled");
    }
  }

  function start() {
    if (state.active) return;
    state.active = true;
    mountOverlay();
    document.documentElement.style.cursor = "crosshair";
    document.addEventListener("mousemove", onMouseMove, true);
    document.addEventListener("click", onClick, true);
    document.addEventListener("keydown", onKeyDown, true);
  }

  chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
    if (msg && msg.type === "track-elem:start") {
      start();
      sendResponse({ ok: true });
    }
  });
})();
