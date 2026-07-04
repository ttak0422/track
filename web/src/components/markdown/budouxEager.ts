import { loadDefaultJapaneseParser } from "budoux";
import { makeRehypeBudoux } from "./plugins";

// The BudouX parser (which pulls its ~190KB Japanese model) is created on first use, not at module load,
// so this module has no top-level side effect and the bundler can drop it entirely when unused. It is
// referenced only behind __TRACK_STATIC__ in MarkdownView, so the static help site tree-shakes BudouX
// away (English content is never segmented) while the live Japanese workspace keeps word-breaking.
let parser: ReturnType<typeof loadDefaultJapaneseParser> | null = null;

export const rehypeBudoux = /*#__PURE__*/ makeRehypeBudoux(
  (text) => (parser ??= loadDefaultJapaneseParser()).parse(text),
);
