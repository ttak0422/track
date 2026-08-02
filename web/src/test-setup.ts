// Extends vitest's expect with @testing-library/jest-dom matchers (toBeInTheDocument, toBeChecked, …)
// and cleans up the DOM between tests.
import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// jsdom still lacks <dialog>'s showModal/close; give tests the minimal behavior the app relies on
// (the open attribute and the close event) so modal components are testable.
HTMLDialogElement.prototype.showModal ??= function (this: HTMLDialogElement) {
  this.open = true;
};
HTMLDialogElement.prototype.close ??= function (this: HTMLDialogElement) {
  this.open = false;
  this.dispatchEvent(new Event("close"));
};

// jsdom does no layout and so ships no scrollIntoView; components that keep an element in view call it
// unconditionally. There is nothing to assert about scrolling here, so a no-op is enough.
Element.prototype.scrollIntoView ??= () => {};

// jsdom ships no ResizeObserver; Kumo's Chart constructs one unconditionally. jsdom does no layout,
// so a no-op observer (nothing ever resizes) is the honest behavior.
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

afterEach(() => {
  cleanup();
});
