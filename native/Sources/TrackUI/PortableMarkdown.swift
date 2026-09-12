import Foundation

// Swift port of web/src/components/markdown/portable.ts (toPortableMarkdown only).
// Strips track-specific link constructs so the body pastes cleanly elsewhere:
//   [[key]] / [[key|alias]]    -> the alias, else the key (heading anchor dropped)
//   ![[Note##h:only-contents]] -> the referenced title as plain text (include NOT expanded)
// Ordinary Markdown passes through untouched. Like the original this is
// regex-level flattening: a literal [[x]] inside a fenced code block is also
// flattened, accepted until someone actually pastes that.
public enum PortableMarkdown {
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
