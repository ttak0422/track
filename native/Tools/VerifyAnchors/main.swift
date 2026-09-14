import Foundation

@main
struct VerifyAnchors {
    static func main() {
        let headings = MarkdownAnchors.headings("# 設計\n## 設計\n```\n# Hidden\n```\n# See [[Note|別名]] *text*\n~~~swift\n# Hidden too\n~~~\n# ???")
        precondition(headings.map(\.id) == ["h-設計", "h-設計-2", "h-see-別名-text", "h-section"])
        precondition(MarkdownAnchors.headings("````\n```\n# hidden\n````\n# visible").map(\.title) == ["visible"])
        precondition(MarkdownAnchors.headings("    # indented code\n# real").count == 1)
        precondition(MarkdownAnchors.target("other:Note##設計").key == "other:Note")
        precondition(MarkdownAnchors.target("other:Note##設計").anchor == "h-設計")
        precondition(MarkdownAnchors.target("#^Block-1").anchor == "block-Block-1")
        precondition(MarkdownAnchors.target("Note#^not an id").anchor == "h-not-an-id")
        precondition(MarkdownAnchors.target("C#").key == "C#")
        precondition(MarkdownAnchors.target("C###").anchor == nil)
        let wiki = "other:日本語/100% and %20##設計"
        precondition(MarkdownAnchors.wikiTarget(MarkdownAnchors.wikiURL(wiki)) == wiki)
        let source = """
        # Start
        First[^b] and again[^b], then[^a].

        [^a]: A definition
        [^b]: B definition
            continued

        - [x] completed task
        Paragraph ^Block-1
        `literal [^b]` and \\[^a]
        ~~~
        [^code]: stay literal
        # Not a heading
        ~~~
        """
        let document = MarkdownAnchors.prepare(source)
        precondition(document.lines[7] == "- [x] completed task", "Footnotes moved the task's source line")
        precondition(document.lines[3].isEmpty && document.lines[4].isEmpty && document.lines[5].isEmpty)
        precondition(document.lines[9] == "`literal [^b]` and \\[^a]")
        precondition(document.lines[11] == "[^code]: stay literal")
        precondition(document.lines[1] == "First[1](trackanchor://jump/fn-1) and again[1](trackanchor://jump/fn-1), then[2](trackanchor://jump/fn-2).")
        precondition(document.anchors[1]!.filter { $0.hasPrefix("fnref-") } == ["fnref-1-1", "fnref-1-2", "fnref-2-1"])
        precondition(document.anchors[8] == ["block-Block-1"] && document.lines[8] == "Paragraph")
        let footnoteStart = document.anchors.first { $0.value.contains("fn-1") }!.key
        precondition(document.lines[footnoteStart] == "**1.** B definition")
        precondition(document.lines[footnoteStart + 1].contains("continued"))
        precondition(document.lines[footnoteStart + 1].contains("trackanchor://jump/fnref-1-1"))
        precondition(document.lines[footnoteStart + 1].contains("trackanchor://jump/fnref-1-2"))
        precondition(MarkdownAnchors.prepare("^alone\ntext \\^escaped\n`text ^code`").anchors.values.flatMap { $0 }.allSatisfy { !$0.hasPrefix("block-") })
        let raw = "# Start\n\nFirst paragraph\n\n```query\nquery source\n```\n\nLast paragraph"
        let rendered = "# Start\n\nFirst paragraph\n\n```chart\ngenerated 1\ngenerated 2\ngenerated 3\n```\n\nLast paragraph"
        precondition(MarkdownAnchors.sourceBlocks("1. First\nlazy continuation\n\n1. Second") == [0])
        precondition(MarkdownAnchors.sourceBlocks("1. First\n\n1. Second\n\n    continued\n\nAfter") == [0, 6])
        precondition(MarkdownAnchors.sourceBlocks(raw) == [0, 2, 4, 8])
        precondition(MarkdownAnchors.sourceTarget(source: raw, rendered: raw, line: 6) == "source-line-5")
        precondition(MarkdownAnchors.sourceTarget(source: raw, rendered: rendered, line: 9) == "source-line-11")
        precondition(MarkdownAnchors.sourceTarget(source: raw, rendered: rendered, line: 6) == "source-line-5")
        precondition(MarkdownAnchors.sourceTarget(source: raw, rendered: rendered.replacingOccurrences(of: "generated 1", with: "generated 1\n\n"), line: 6) == "source-line-5")
        let sourceMap = MarkdownAnchors.sourceLines(source: raw, rendered: rendered)
        precondition(sourceMap[10] == 8 && sourceMap[5] == nil)
        precondition(MarkdownAnchors.sourceLines(source: "- [ ] real", rendered: "- [ ] real\n- [ ] real").allSatisfy { $0 == nil })
        precondition(MarkdownAnchors.sourceLines(source: "a\nb", rendered: "a\nb") == [0, 1])
        precondition(MarkdownAnchors.sourceTarget(source: raw, rendered: raw, line: -10) == "source-line-1")
        precondition(MarkdownAnchors.sourceTarget(source: raw, rendered: raw, line: 999) == "source-line-9")
        precondition(MarkdownAnchors.sourceTarget(source: "", rendered: "", line: 1) == nil)
        precondition(document.anchors[footnoteStart]!.contains("source-line-5"))
        print("Anchor checks passed: Japanese/duplicate headings, fences, vault/level grammar, blocks, footnote order/backlinks, source-line stability.")
    }
}
