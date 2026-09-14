import Foundation

/// IDs mirror web/markdown/{toc,plugins}.ts. Source lines stay in place so
/// anchors and native task edits address the same document.
public enum MarkdownAnchors {
    public struct Heading: Identifiable, Equatable {
        public let id: String
        public let line: Int
        public let level: Int
        public let title: String
    }

    public struct Document {
        public var lines: [String]
        public var anchors: [Int: [String]]
        public let headings: [Heading]
    }

    public static func wikiURL(_ target: String) -> URL {
        var components = URLComponents()
        components.scheme = "trackwiki"
        components.host = "jump"
        components.path = "/" + target
        return components.url!
    }

    public static func wikiTarget(_ url: URL) -> String? {
        guard url.scheme?.lowercased() == "trackwiki" else { return nil }
        if url.host == "jump" { return String(url.path.dropFirst()) }
        let legacy = (url.host ?? "") + url.path
        return legacy.removingPercentEncoding ?? legacy
    }

    public static func target(_ wiki: String) -> (key: String, anchor: String?) {
        guard let hash = wiki.firstIndex(of: "#") else { return (wiki, nil) }
        let rest = String(wiki[wiki.index(after: hash)...]).trimmingCharacters(in: .whitespaces)
        if rest.range(of: #"^\^[A-Za-z0-9][A-Za-z0-9_-]*$"#, options: .regularExpression) != nil {
            return (String(wiki[..<hash]).trimmingCharacters(in: .whitespaces), "block-" + rest.dropFirst())
        }
        let heading = rest.drop(while: { $0 == "#" }).trimmingCharacters(in: .whitespaces)
        guard !heading.isEmpty else { return (wiki, nil) }
        // Web intentionally resolves every heading level to the first text match.
        return (String(wiki[..<hash]).trimmingCharacters(in: .whitespaces), "h-" + slug(heading))
    }

    public static func headings(_ source: String) -> [Heading] {
        var seen: [String: Int] = [:]
        return proseLines(source.components(separatedBy: "\n")).compactMap { index, line in
            guard let match = matches(#"^(#{1,6})\s+(.+?)\s*#*\s*$"#, line).first else { return nil }
            let title = label(match[2]), base = slug(title)
            seen[base, default: 0] += 1
            let occurrence = seen[base]!
            return Heading(id: "h-" + base + (occurrence == 1 ? "" : "-\(occurrence)"),
                           line: index, level: match[1].count, title: title)
        }
    }

    public static func prepare(_ source: String) -> Document {
        var lines = source.components(separatedBy: "\n")
        let outline = headings(source)
        var anchors = Dictionary(uniqueKeysWithValues: outline.map { ($0.line, [$0.id]) })
        let prose = proseLines(lines)
        var definitions: [String: String] = [:]
        var definitionLines = Set<Int>()
        for (index, line) in prose {
            guard let match = matches(#"^ {0,3}\[\^([^\]\s]+)\]:[ \t]*(.*)$"#, line).first else { continue }
            var text = match[2]
            definitionLines.insert(index)
            var next = index + 1
            while next < lines.count {
                if lines[next].hasPrefix("    ") || lines[next].hasPrefix("\t") {
                    text += "\n" + String(lines[next].dropFirst(lines[next].hasPrefix("\t") ? 1 : 4))
                    definitionLines.insert(next)
                    next += 1
                } else if lines[next].isEmpty, next + 1 < lines.count,
                          lines[next + 1].hasPrefix("    ") || lines[next + 1].hasPrefix("\t") {
                    text += "\n"
                    definitionLines.insert(next)
                    next += 1
                } else { break }
            }
            if definitions[match[1]] == nil { definitions[match[1]] = text }
        }
        var order: [String] = []
        var references: [String: [String]] = [:]
        for (index, original) in prose where !definitionLines.contains(index) {
            var line = transformProse(original) { text in
                replace(#"(?<!\\)\[\^([^\]\s]+)\]"#, in: text) { groups in
                    let id = groups[1]
                    guard definitions[id] != nil else { return groups[0] }
                    if references[id] == nil { order.append(id) }
                    let number = order.firstIndex(of: id)! + 1
                    let ref = "fnref-\(number)-\((references[id]?.count ?? 0) + 1)"
                    references[id, default: []].append(ref)
                    anchors[index, default: []].append(ref)
                    return "[\(number)](trackanchor://jump/fn-\(number))"
                }
            }
            if let match = matches(#"[ \t]+\^([A-Za-z0-9][A-Za-z0-9_-]*)[ \t]*$"#, line).first,
               !line.trimmingCharacters(in: .whitespaces).hasPrefix("#") {
                let stripped = String(line.dropLast(match[0].count))
                if !stripped.trimmingCharacters(in: .whitespaces).isEmpty {
                    anchors[index, default: []].append("block-" + match[1])
                    line = stripped
                }
            }
            lines[index] = line
        }
        for index in definitionLines { lines[index] = "" }
        for (offset, id) in order.enumerated() {
            lines.append("")
            anchors[lines.count] = ["fn-\(offset + 1)"]
            let backs = references[id]!.enumerated().map { index, ref in
                "[↩\(index == 0 ? "" : String(index + 1))](trackanchor://jump/\(ref))"
            }.joined(separator: " ")
            lines.append(contentsOf: "**\(offset + 1).** \(definitions[id]!)  \(backs)".components(separatedBy: "\n"))
        }
        return Document(lines: lines, anchors: anchors, headings: outline)
    }

    /// Fence-aware source scan, shared by heading IDs and the segmenter.
    static func proseLines(_ lines: [String]) -> [(Int, String)] {
        var fence: String?
        var result: [(Int, String)] = []
        for (index, line) in lines.enumerated() {
            let marker = matches(#"^ {0,3}(`{3,}|~{3,})"#, line).first?[1]
            if let current = fence {
                if let marker, marker.first == current.first, marker.count >= current.count { fence = nil }
                continue
            }
            if let marker { fence = marker; continue }
            if line.hasPrefix("    ") || line.hasPrefix("\t") { continue }
            result.append((index, line))
        }
        return result
    }

    static func label(_ source: String) -> String {
        var value = replace(#"\[\[([^|\]]+)(?:\|([^\]]+))?\]\]"#, in: source) { $0[2].isEmpty ? $0[1] : $0[2] }
        value = replace(#"\[([^\]]+)\]\([^\s)]*\)"#, in: value) { $0[1] }
        return value.filter { !"*_~`".contains($0) }.trimmingCharacters(in: .whitespaces)
    }

    static func slug(_ value: String) -> String {
        let clean = label(value).lowercased().filter { $0.isLetter || $0.isNumber || $0.isWhitespace || $0 == "-" }
        let result = clean.split(whereSeparator: \.isWhitespace).joined(separator: "-")
        return result.isEmpty ? "section" : result
    }

    /// Inline code and escaped markers retain their literal source.
    static func transformProse(_ line: String, _ transform: (String) -> String) -> String {
        var result = "", cursor = line.startIndex
        while let start = line[cursor...].firstIndex(of: "`") {
            let run = line[start...].prefix(while: { $0 == "`" })
            let after = line.index(start, offsetBy: run.count)
            guard let close = line.range(of: String(run), range: after..<line.endIndex) else { break }
            result += transform(String(line[cursor..<start]))
            result += line[start..<close.upperBound]
            cursor = close.upperBound
        }
        return result + transform(String(line[cursor...]))
    }

    private static func matches(_ pattern: String, _ text: String) -> [[String]] {
        let regex = try! NSRegularExpression(pattern: pattern)
        return regex.matches(in: text, range: NSRange(text.startIndex..., in: text)).map { match in
            (0..<match.numberOfRanges).map { index in
                Range(match.range(at: index), in: text).map { String(text[$0]) } ?? ""
            }
        }
    }

    private static func replace(_ pattern: String, in text: String, _ transform: ([String]) -> String) -> String {
        let regex = try! NSRegularExpression(pattern: pattern)
        var output = "", cursor = text.startIndex
        for match in regex.matches(in: text, range: NSRange(text.startIndex..., in: text)) {
            guard let range = Range(match.range, in: text) else { continue }
            output += text[cursor..<range.lowerBound]
            output += transform((0..<match.numberOfRanges).map { Range(match.range(at: $0), in: text).map { String(text[$0]) } ?? "" })
            cursor = range.upperBound
        }
        return output + text[cursor...]
    }
}
