import Foundation

// Runs only in WKContentWorld.defaultClient. No page API or native message handler is exposed.
enum PageFindScript {
    static let source = #"""
    (() => {
      if (globalThis.__trackPageFind) return;
      let query = '', current = -1, ranges = [], dirty = true, route = '', observer, scope;
      const supported = typeof Highlight !== 'undefined' && !!globalThis.CSS?.highlights;
      const excluded = 'script,style,noscript,input,textarea,select,[contenteditable]:not([contenteditable="false"]),[hidden],[aria-hidden="true"],.note-aside,.line-hint,.note-stamps';
      const root = () => document.querySelector('.reader') || document.body;
      function collect() {
        const runs = [];
        let text = '', parts = [];
        const flush = () => { if (text) runs.push({ text, parts }); text = ''; parts = []; };
        function visit(node) {
          if (node.nodeType === Node.TEXT_NODE) {
            if (node.data) { parts.push({ node, start: text.length }); text += node.data; }
            return;
          }
          if (!(node instanceof Element) || node.matches(excluded)) return;
          const style = getComputedStyle(node);
          if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse' || style.opacity === '0') return;
          if (node.tagName === 'BR') { text += '\n'; return; }
          const block = style.display && !style.display.startsWith('inline') && style.display !== 'contents';
          if (block) flush();
          for (const child of node.childNodes) visit(child);
          if (block) flush();
        }
        visit(root()); flush();
        // Literal, case-insensitive page search. Whitespace can span line breaks and inline markup.
        const escaped = query.split(/(\s+)/u).map(part => /^\s+$/u.test(part) ? '\\s+' : part.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('');
        const matcher = new RegExp(escaped, 'giu');
        const found = [];
        if (!query.trim()) return found;
        for (const run of runs) {
          let first = 0;
          for (const match of run.text.matchAll(matcher)) {
            const start = match.index, end = start + match[0].length;
            while (first < run.parts.length && run.parts[first].start + run.parts[first].node.length <= start) first++;
            let last = first;
            while (last + 1 < run.parts.length && run.parts[last + 1].start < end) last++;
            const a = run.parts[first], b = run.parts[last];
            if (!a || !b || start < a.start || end > b.start + b.node.length) continue;
            const range = document.createRange();
            range.setStart(a.node, start - a.start); range.setEnd(b.node, end - b.start);
            if (range.getClientRects().length) found.push(range);
          }
        }
        return found;
      }
      function paint() {
        if (!supported) return;
        if (!document.getElementById('track-native-find-style')) {
          const style = document.createElement('style'); style.id = 'track-native-find-style';
          style.textContent = `::highlight(track-find-all) { color: var(--text, CanvasText); background-color: var(--panel-soft, Mark); }
            ::highlight(track-find-current) { color: var(--panel, HighlightText); background-color: var(--mark, Highlight); }`;
          document.head.append(style);
        }
        const all = new Highlight(); for (const range of ranges) all.add(range);
        const active = new Highlight(); if (ranges[current]) active.add(ranges[current]);
        active.priority = 1;
        CSS.highlights.set('track-find-all', all); CSS.highlights.set('track-find-current', active);
      }
      function reveal(range) {
        // Scroll the range itself, even when several matches share one very tall paragraph.
        for (let element = range.startContainer.parentElement; element; element = element.parentElement) {
          const style = getComputedStyle(element);
          if (!/(auto|scroll)/.test(style.overflowY) || element.scrollHeight <= element.clientHeight) continue;
          const rect = range.getBoundingClientRect(), box = element.getBoundingClientRect();
          const top = box.top + element.clientTop + 12, bottom = box.top + element.clientTop + element.clientHeight - 12;
          if (rect.top < top) element.scrollTop += rect.top - top;
          else if (rect.bottom > bottom) element.scrollTop += rect.bottom - bottom;
        }
        const rect = range.getBoundingClientRect();
        const top = (document.querySelector('.tabstrip')?.getBoundingClientRect().bottom || 0) + 12;
        if (rect.top < top) window.scrollBy(0, rect.top - top);
        else if (rect.bottom > innerHeight - 12) window.scrollBy(0, rect.bottom - innerHeight + 12);
      }
      function snapshot() {
        const range = ranges[current], rect = range?.getBoundingClientRect();
        const scheme = getComputedStyle(document.documentElement).colorScheme;
        return { supported, count: ranges.length, index: current, text: range?.toString() || '',
          id: range?.startContainer.parentElement?.id || '', x: rect?.x || 0, y: rect?.y || 0,
          width: rect?.width || 0, height: rect?.height || 0,
          tabBottom: document.querySelector('.tabstrip')?.getBoundingClientRect().bottom || 0,
          dark: scheme === 'dark' || (scheme !== 'light' && matchMedia('(prefers-color-scheme: dark)').matches) };
      }
      function clear() {
        observer?.disconnect(); observer = undefined;
        query = ''; current = -1; ranges = []; dirty = true;
        if (supported) { CSS.highlights.delete('track-find-all'); CSS.highlights.delete('track-find-current'); }
        return snapshot();
      }
      function update(value, delta = 0, restart = false) {
        if (!supported) return snapshot();
        if (!value) return clear();
        if (scope !== root()) { scope = root(); dirty = true; }
        const path = location.pathname + location.search;
        const reset = restart || value !== query || path !== route;
        if (!observer) {
          observer = new MutationObserver(records => {
            if (records.some(record => scope?.contains(record.target))) dirty = true;
          });
          observer.observe(document.body, { childList: true, characterData: true, subtree: true,
            attributes: true, attributeFilter: ['hidden', 'aria-hidden', 'open', 'class', 'style'] });
        }
        if (reset || dirty) {
          const previous = ranges[current];
          query = value; route = path; ranges = collect(); dirty = false;
          const same = reset || !previous ? -1 : ranges.findIndex(range => range.startContainer === previous.startContainer && range.startOffset === previous.startOffset);
          current = ranges.length ? (reset ? 0 : same >= 0 ? same : Math.max(0, Math.min(current, ranges.length - 1))) : -1;
          paint();
        }
        if (delta && ranges.length && !reset) { current = (current + delta + ranges.length) % ranges.length; paint(); }
        else if (reset && delta < 0 && ranges.length) { current = ranges.length - 1; paint(); }
        if ((reset || delta) && ranges[current]) reveal(ranges[current]);
        return snapshot();
      }
      globalThis.__trackPageFind = { update, clear, snapshot };
    })();
    """#
}
