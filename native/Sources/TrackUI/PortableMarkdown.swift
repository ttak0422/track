import Foundation
import AppKit
import cmark_gfm
import cmark_gfm_extensions

// Swift port of web/src/components/markdown/portable.ts (toPortableMarkdown only).
// Strips track-specific link constructs so the body pastes cleanly elsewhere:
//   [[key]] / [[key|alias]]    -> the alias, else the key (heading anchor dropped)
//   ![[Note##h:only-contents]] -> the referenced title as plain text (include NOT expanded)
// Ordinary Markdown passes through untouched. Like the original this is
// regex-level flattening: a literal [[x]] inside a fenced code block is also
// flattened, accepted until someone actually pastes that.
public enum PortableMarkdown {
    /// Reuse the reader's cmark-gfm parser directly. MarkdownUI's public HTML
    /// export rebuilds its table nodes without column metadata and loses cells.
    public static func html(_ markdown: String) -> String {
        cmark_gfm_core_extensions_ensure_registered()
        guard let parser = cmark_parser_new(CMARK_OPT_DEFAULT) else { return "" }
        defer { cmark_parser_free(parser) }
        for name in ["table", "autolink", "strikethrough", "tagfilter", "tasklist"] {
            if let syntax = cmark_find_syntax_extension(name) { cmark_parser_attach_syntax_extension(parser, syntax) }
        }
        let text = portable(markdown)
        cmark_parser_feed(parser, text, text.utf8.count)
        guard let document = cmark_parser_finish(parser) else { return "" }
        defer { cmark_node_free(document) }
        // The web allows <br> as a line break while omitting arbitrary HTML.
        // Work on HTML nodes so a literal <br> inside code stays literal.
        if let iterator = cmark_iter_new(document) {
            var breaks: [UnsafeMutablePointer<cmark_node>] = []
            while cmark_iter_next(iterator) != CMARK_EVENT_DONE {
                if cmark_iter_get_event_type(iterator) == CMARK_EVENT_ENTER,
                   let node = cmark_iter_get_node(iterator), cmark_node_get_type(node) == CMARK_NODE_HTML_INLINE,
                   let literal = cmark_node_get_literal(node),
                   String(cString: literal).range(of: #"^<br\s*/?>$"#, options: [.regularExpression, .caseInsensitive]) != nil {
                    breaks.append(node)
                }
            }
            cmark_iter_free(iterator)
            for node in breaks {
                if let replacement = cmark_node_new(CMARK_NODE_LINEBREAK) {
                    cmark_node_insert_before(node, replacement)
                    cmark_node_unlink(node)
                    cmark_node_free(node)
                }
            }
        }
        guard let result = cmark_render_html(document, CMARK_OPT_DEFAULT, cmark_parser_get_syntax_extensions(parser)) else { return "" }
        defer { free(result) }
        return String(cString: result)
    }

    /// A native selection carries attributed text rather than source offsets.
    /// Preserve selected link/emphasis runs and table cells when reconstructing
    /// Markdown; never widen a partial selection to the entire note.
    public static func selectedMarkdown(_ selection: NSAttributedString) -> String {
        var output = ""
        var tablePosition: (row: Int, column: Int)?
        selection.enumerateAttributes(in: NSRange(location: 0, length: selection.length)) { attributes, range, _ in
            var text = (selection.string as NSString).substring(with: range)
            let paragraph = attributes[.paragraphStyle] as? NSParagraphStyle
            if let cell = paragraph?.textBlocks.first as? NSTextTableBlock {
                if let previous = tablePosition {
                    if previous.row != cell.startingRow { output += " |\n| " }
                    else if previous.column != cell.startingColumn { output += " | " }
                } else { output += "| " }
                tablePosition = (cell.startingRow, cell.startingColumn)
                text = text.trimmingCharacters(in: .newlines).replacingOccurrences(of: "|", with: "\\|")
            } else if tablePosition != nil {
                output += " |\n"
                tablePosition = nil
            }
            if let url = attributes[.link] as? URL {
                if let target = MarkdownAnchors.wikiTarget(url) { text = "[[\(target)|\(text)]]" }
                else { text = "[\(text)](\(url.absoluteString))" }
            } else if let link = attributes[.link] as? String {
                text = "[\(text)](\(link))"
            }
            if let font = attributes[.font] as? NSFont {
                if font.fontDescriptor.symbolicTraits.contains(.bold) { text = "**\(text)**" }
                if font.fontDescriptor.symbolicTraits.contains(.italic) { text = "*\(text)*" }
            }
            if let strike = attributes[.strikethroughStyle] as? Int, strike != 0 { text = "~~\(text)~~" }
            output += text
        }
        if tablePosition != nil { output += " |" }
        return output
    }

    public static func portable(_ body: String) -> String {
        let withoutIncludes = flattenIncludes(body)
        return flattenWikilinks(withoutIncludes)
    }

    /// Drops the GFM separator row from every table (web stripTableDelimiterRows):
    /// a Markdown copy carries the cell values, not the rule that turns source
    /// into a table. The range slicer shares this because Confluence HTML still
    /// needs the rule for its own table parsing.
    public static func stripTableDelimiterRows(_ markdown: String) -> String {
        let pattern = #"^\s*\|?\s*:?-{3,}:?\s*(?:\|\s*:?-{3,}:?\s*)+\|?\s*$"#
        guard let regex = try? NSRegularExpression(pattern: pattern, options: []) else { return markdown }
        return markdown
            .split(separator: "\n", omittingEmptySubsequences: false)
            .filter { line in
                let s = String(line)
                return regex.firstMatch(in: s, options: [], range: NSRange(s.startIndex..., in: s)) == nil
            }
            .joined(separator: "\n")
    }

    /// The plain-text fallback for the Confluence copy (web confluencePlainText):
    /// delimiter-free Markdown with literal <br> as real line breaks, while the
    /// HTML flavor keeps them untouched.
    public static func confluencePlainText(_ portable: String) -> String {
        stripTableDelimiterRows(portable).replacingOccurrences(
            of: #"<br\s*/?>"#, with: "\n",
            options: [.regularExpression, .caseInsensitive]
        )
    }

    private static func flattenIncludes(_ body: String) -> String {
        // ^([ \t]*)!\[\[([^[\]]+)\]\][ \t]*.*$ (multiline) -> "$1[[$2]]"
        guard let pattern = try? NSRegularExpression(
            pattern: "^([ \\t]*)!\\[\\[([^\\[\\]]+)\\]\\][ \\t]*.*$",
            options: [.anchorsMatchLines]
        ) else { return body }
        let range = NSRange(body.startIndex..., in: body)
        return pattern.stringByReplacingMatches(
            in: body, options: [], range: range, withTemplate: "$1[[$2]]"
        )
    }

    private static func flattenWikilinks(_ body: String) -> String {
        // \[\[([^\]|]+)(?:\|([^\]]+))?\]\]
        guard let pattern = try? NSRegularExpression(
            pattern: "\\[\\[([^\\]|]+)(?:\\|([^\\]]+))?\\]\\]", options: []
        ) else { return body }
        let ns = body as NSString
        let matches = pattern.matches(in: body, options: [], range: NSRange(location: 0, length: ns.length))
        guard !matches.isEmpty else { return body }
        var out = ""
        var last = body.startIndex
        for match in matches {
            let fullRange = Range(match.range, in: body)!
            out += body[last..<fullRange.lowerBound]
            let target = String(body[Range(match.range(at: 1), in: body)!])
            var alias: String? = nil
            let aliasRange = match.range(at: 2)
            if aliasRange.location != NSNotFound, let r = Range(aliasRange, in: body) {
                alias = String(body[r])
            }
            let label = alias?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            if !label.isEmpty {
                out += label
            } else {
                let key = target.split(separator: "#", maxSplits: 1, omittingEmptySubsequences: false).first.map(String.init) ?? target
                out += key.trimmingCharacters(in: .whitespacesAndNewlines)
            }
            last = fullRange.upperBound
        }
        out += body[last...]
        return out
    }
}
