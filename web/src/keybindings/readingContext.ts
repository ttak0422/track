// These surfaces retain their own keys, including non-editable controls and custom widgets.
const interactive = [
  "input", "textarea", "select", "button", "a[href]", "summary", "iframe", "object", "embed",
  "audio", "video", "canvas", "[contenteditable]:not([contenteditable='false'])", "[tabindex]",
  "[role=button]", "[role=link]", "[role=textbox]", "[role=combobox]", "[role=slider]",
  "[role=spinbutton]", "[role=checkbox]", "[role=radio]", "[role=menuitem]", "[role=tree]",
].join(",");

export function hasReadingOverlay(document: Document): boolean {
  return document.querySelector("[role=dialog], [role=alertdialog], [aria-modal=true], dialog[open], [role=menu], .menu-panel, .rail-menu-panel, .selection-copy") !== null;
}

export function isReadingTextTarget(target: EventTarget | null, reader: HTMLElement): boolean {
  if (!(target instanceof Element) || !reader.contains(target)) return false;
  // Walk ancestors to catch a span inside a button, inherited contenteditable, and nested widgets.
  for (let element: Element | null = target; element && element !== reader; element = element.parentElement) {
    if (element.matches(interactive)) return false;
  }
  return target === reader || target.closest(".note-preview") !== null;
}
