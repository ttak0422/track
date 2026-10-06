import type { Pluggable } from "unified";

export interface MathPlugins {
  remark: Pluggable;
  rehype: Pluggable;
}

let cached: MathPlugins | null = null;
let promise: Promise<MathPlugins> | null = null;

// loadMathPlugins dynamically imports remark-math, rehype-katex, and KaTeX's stylesheet, so the ~270KB
// KaTeX bundle is fetched only for notes that actually contain $…$ math instead of shipping in the main
// chunk. The result is cached: the first math note in a session pays the load, the rest are instant.
// package.json narrowly aligns rehype-katex@7.0.1 and mermaid@11.16.1 with the direct KaTeX version.
// Both use public renderToString, but declare 0.16.x ranges. KaTeX 0.18's renamed layout classes
// require their HTML output (including Mermaid's optional legacy math) and this CSS to stay together.
// Integration tests cover that contract and error/trust limits. Recheck overrides on adapter updates.
export function loadMathPlugins(): Promise<MathPlugins> {
  promise ??= (async () => {
    const [remark, rehype] = await Promise.all([
      import("remark-math").then((m) => m.default as Pluggable),
      import("rehype-katex").then((m) => m.default as Pluggable),
      import("katex/dist/katex.min.css").then(() => undefined),
    ]);
    cached = { remark, rehype };
    return cached;
  })();
  return promise;
}

// mathPluginsIfLoaded returns the plugins synchronously once they have loaded this session, so a later
// math note renders straight away without a flash of raw "$…$" source.
export function mathPluginsIfLoaded(): MathPlugins | null {
  return cached;
}

// looksLikeMath is a cheap pre-check for whether a body is worth loading KaTeX for: a $$…$$ block or a
// $…$ inline span. Over-triggering only costs an unnecessary chunk load; under-triggering would leave
// real math unrendered, so it errs toward matching.
export function looksLikeMath(markdown: string): boolean {
  return /\$\$[\s\S]*?\$\$|\$[^$\n]+\$/.test(markdown);
}
