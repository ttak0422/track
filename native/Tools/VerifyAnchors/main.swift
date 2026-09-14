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
        precondition(document.anchors[1] == ["fnref-1-1", "fnref-1-2", "fnref-2-1"])
        precondition(document.anchors[8] == ["block-Block-1"] && document.lines[8] == "Paragraph")
        let footnoteStart = document.anchors.first { $0.value == ["fn-1"] }!.key
        precondition(document.lines[footnoteStart] == "**1.** B definition")
        precondition(document.lines[footnoteStart + 1].contains("continued"))
        precondition(document.lines[footnoteStart + 1].contains("trackanchor://jump/fnref-1-1"))
        precondition(document.lines[footnoteStart + 1].contains("trackanchor://jump/fnref-1-2"))
        precondition(MarkdownAnchors.prepare("^alone\ntext \\^escaped\n`text ^code`").anchors.isEmpty)
        print("Anchor checks passed: Japanese/duplicate headings, fences, vault/level grammar, blocks, footnote order/backlinks, source-line stability.")
    }
}
