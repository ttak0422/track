import { useContext } from "react";
import { MarkdownSourceContext } from "./context";
import { headingTree, layoutMindmap, mindmapNodeHeight, outlineTree } from "./mindmap";

interface MindmapDiagramProps {
  text: string;
}

// MindmapDiagram renders a fenced ```mindmap block as a left-to-right tree of rounded nodes. An empty
// fence maps the surrounding note's heading tree; a non-empty fence is an indented outline (one node
// per line). The SVG is plain React elements — no async engine — so the static prerender emits the
// finished diagram and it is visible before any script runs. Colors come from the theme's CSS
// variables, so it follows light/dark for free.
export function MindmapDiagram({ text }: MindmapDiagramProps) {
  const source = useContext(MarkdownSourceContext);
  const tree = text.trim() === "" ? headingTree(source) : outlineTree(text);
  if (!tree) {
    return <p className="muted">Mindmap: nothing to map (no headings or outline lines).</p>;
  }

  const pad = 8;
  const { width, height, nodes, edges } = layoutMindmap(tree);
  return (
    <div className="mindmap-diagram">
      <svg
        viewBox={`${-pad} ${-pad} ${width + 2 * pad} ${height + 2 * pad}`}
        width="100%"
        style={{ maxWidth: `${width + 2 * pad}px` }}
        role="img"
        aria-label="Mindmap"
      >
        {edges.map((edge) => {
          const from = nodes[edge.from];
          const to = nodes[edge.to];
          const x1 = from.x + from.w;
          const y1 = from.y + from.h / 2;
          const x2 = to.x;
          const y2 = to.y + to.h / 2;
          const mid = (x1 + x2) / 2;
          return (
            <path
              key={`${edge.from}-${edge.to}`}
              className="mindmap-edge"
              d={`M ${x1} ${y1} C ${mid} ${y1}, ${mid} ${y2}, ${x2} ${y2}`}
            />
          );
        })}
        {nodes.map((node, i) => (
          <g key={i} className={node.depth === 0 ? "mindmap-node mindmap-root" : "mindmap-node"}>
            <rect x={node.x} y={node.y} width={node.w} height={node.h} rx={mindmapNodeHeight / 2} />
            {node.label !== "" && (
              <text x={node.x + node.w / 2} y={node.y + node.h / 2}>
                {node.label}
              </text>
            )}
          </g>
        ))}
      </svg>
    </div>
  );
}
