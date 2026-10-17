import { createContext } from "react";
import type { DrawioFence } from "../../types";

export const DRAWIO_ORIGIN = "https://embed.diagrams.net";
export const DRAWIO_URL = `${DRAWIO_ORIGIN}/?embed=1&proto=json&spin=1&libraries=1&saveAndExit=0&noExitBtn=1&modified=0`;
export const MAX_DRAWIO_XML = 4 * 1024 * 1024;

export interface DrawioEditing {
  fences: DrawioFence[];
  open: (fence: DrawioFence) => void;
}
export const DrawioEditingContext = createContext<DrawioEditing | null>(null);

// Validate without reserializing: all pages, layers, geometry and compressed
// payloads belong to the source document, not the viewer's first-page DOM.
export function validDrawioXML(xml: unknown): xml is string {
  if (typeof xml !== "string" || !xml.trim() || xml.length > MAX_DRAWIO_XML ||
      new TextEncoder().encode(xml).length > MAX_DRAWIO_XML || /<!DOCTYPE|<!ENTITY/i.test(xml)) return false;
  const doc = new DOMParser().parseFromString(xml, "text/xml");
  return !doc.querySelector("parsererror") && ["mxfile", "mxGraphModel"].includes(doc.documentElement.tagName);
}

export function replaceDrawioFence(body: string, snapshot: string, fence: DrawioFence, xml: string): string | null {
  if (body !== snapshot || !validDrawioXML(xml)) return null;
  const lines = body.split("\n");
  const opening = lines[fence.start_line];
  const closing = lines[fence.end_line];
  const match = /^(`{3,})[ \t]*drawio(?:\s|$)/i.exec(opening ?? "");
  if (!match || !/^`{3,}\s*$/.test(closing ?? "") ||
      closing.trim().length < match[1].length ||
      lines.slice(fence.start_line + 1, fence.end_line).join("\n") !== fence.xml) return null;
  // The returned XML must not be able to close the surrounding Markdown fence.
  if (xml.split(/\r?\n/).some((line) => new RegExp("^\\s*`{" + match[1].length + ",}\\s*$").test(line))) return null;
  const eol = opening.endsWith("\r") ? "\r\n" : "\n";
  const replacement = xml.replace(/\r?\n/g, eol).replace(/(?:\r?\n)+$/, "");
  return [...lines.slice(0, fence.start_line + 1), replacement + (eol === "\r\n" ? "\r" : ""), ...lines.slice(fence.end_line)].join("\n");
}
