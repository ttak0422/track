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

    /// One anchor per rendered block, rather than splitting Markdown lines
    /// that belong to the same paragraph, list, table, or code fence.
    public static func sourceBlocks(_ source: String) -> [Int] {
        let lines = source.components(separatedBy: "\n")
        var blocks: [Int] = [], fence: String?, startsBlock = true, inList = false
        for (index, line) in lines.enumerated() {
            let marker = matches(#"^ {0,3}(`{3,}|~{3,})"#, line).first?[1]
            if let current = fence {
                if let marker, marker.first == current.first, marker.count >= current.count {
                    fence = nil
                    startsBlock = true
                }
                continue
            }
            if line.trimmingCharacters(in: .whitespaces).isEmpty { startsBlock = true; continue }
            let heading = line.range(of: #"^#{1,6}\s+"#, options: .regularExpression) != nil
            let listItem = line.range(of: #"^ {0,3}(?:[-+*]|[0-9]+[.)])[ \t]+"#, options: .regularExpression) != nil
            let continuesList = inList && !heading && marker == nil && (listItem || !startsBlock || line.hasPrefix("    ") || line.hasPrefix("\t"))
            if (startsBlock && !continuesList) || marker != nil || heading { blocks.append(index) }
            inList = listItem || continuesList
            fence = marker
            startsBlock = heading
        }
        return blocks
    }

    /// Index by rendered 0-based line; nil means generated or ambiguous text.
    /// Changed multiplicity is ambiguous: a generated duplicate must never
    /// mutate the real task whose source happens to have the same words.
    public static func sourceLines(source: String, rendered: String) -> [Int?] {
        let original = source.components(separatedBy: "\n")
        let output = rendered.components(separatedBy: "\n")
        if source == rendered { return original.indices.map { $0 } }
        // ponytail: diff on demand; cache this map if large generated notes
        // make line mapping measurably expensive.
        let difference = output.difference(from: original)
        let removed = Set(difference.removals.map { change in
            if case .remove(let offset, _, _) = change { return offset }
            preconditionFailure("Expected removal")
        })
        let inserted = Set(difference.insertions.map { change in
            if case .insert(let offset, _, _) = change { return offset }
            preconditionFailure("Expected insertion")
        })
        let originalCounts = original.reduce(into: [String: Int]()) { $0[$1, default: 0] += 1 }
        let outputCounts = output.reduce(into: [String: Int]()) { $0[$1, default: 0] += 1 }
        var map = [Int?](repeating: nil, count: output.count)
        for (sourceLine, renderedLine) in zip(original.indices.filter { !removed.contains($0) }, output.indices.filter { !inserted.contains($0) }) {
            let text = output[renderedLine]
            if originalCounts[text] == outputCounts[text] { map[renderedLine] = sourceLine }
        }
        return map
    }

    public static func sourceTarget(source: String, rendered: String, line: Int) -> String? {
        let requestedLine = min(max(0, line - 1), source.components(separatedBy: "\n").count - 1)
        let originalBlocks = sourceBlocks(source)
        guard let requested = originalBlocks.last(where: { $0 <= requestedLine }) ?? originalBlocks.first else { return nil }
        let map = sourceLines(source: source, rendered: rendered)
        let previous = map.lastIndex { $0.map { $0 <= requested } ?? false }
        let exact = previous.map { map[$0] == requested } ?? false
        // A replaced source block lands at the replacement's beginning.
        let renderedLine = previous.map { $0 + (exact ? 0 : 1) } ?? 0
        let blocks = sourceBlocks(rendered)
        let block = exact
            ? blocks.last(where: { $0 <= renderedLine })
            : blocks.first(where: { $0 >= renderedLine }) ?? blocks.last
        return block.map { "source-line-\($0 + 1)" }
    }

    public static func prepare(_ source: String) -> Document {
        var lines = source.components(separatedBy: "\n")
        let outline = headings(source)
        var anchors = Dictionary(uniqueKeysWithValues: outline.map { ($0.line, [$0.id]) })
        for line in sourceBlocks(source) { anchors[line, default: []].append("source-line-\(line + 1)") }
        let prose = proseLines(lines)
        var definitions: [String: String] = [:]
        var definitionLines = Set<Int>()
        var definitionStarts: [String: Int] = [:]
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
            if definitions[match[1]] == nil {
                definitions[match[1]] = text
                definitionStarts[match[1]] = index
            }
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
        for index in definitionLines {
            lines[index] = ""
            anchors[index] = nil
        }
        for (offset, id) in order.enumerated() {
            lines.append("")
            anchors[lines.count] = ["fn-\(offset + 1)", "source-line-\(definitionStarts[id]! + 1)"]
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
