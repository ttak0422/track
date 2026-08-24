import FA2Layout from "graphology-layout-forceatlas2/worker";
import forceAtlas2 from "graphology-layout-forceatlas2";
import { useEffect, useRef } from "react";
import Sigma from "sigma";
import type { Settings } from "sigma/settings";
import { useThemeVersion } from "../hooks/useThemeVersion";
import type { Graph as TrackGraph, NoteID } from "../types";
import { IconRotate2, RailIcon } from "./icons";
import { graphToGraphology } from "./graphToGraphology";
import type { OverviewEdgeAttributes, OverviewNodeAttributes } from "./graphToGraphology";

export interface GraphOverviewSigmaProps {
  graph: TrackGraph;
  onSelect: (noteID: NoteID) => void;
}

// The ForceAtlas2 layout runs off-thread in its web worker while sigma paints it live. Rather than
// chasing a convergence heuristic it never reaches on a hairball, the layout is cut off after this
// many iterations — a fixed tick count, so the same vault always settles the same way.
const LAYOUT_MAX_TICKS = 300;
// Labels render only above this displayed-size threshold, so at rest only the hubs (the larger size
// grades) are named and zooming in names more of the field.
const LABEL_SIZE_THRESHOLD = 9;

// The tokens the overview draws with, resolved once per theme (design.md keeps every color in a
// custom property; getComputedStyle hands back the resolved rgb() because they are all registered).
interface ThemeColors {
  // Edge ink.
  lineStrong?: string;
  // The salient: the centre node alone.
  mark?: string;
  // Label ink, drawn by sigma's canvas label layer rather than the WebGL programs.
  text?: string;
  // Resolved alongside the rest so theme handling stays one code path; the WebGL disc program is
  // single-fill, so the sheet grounds (--bg/--panel) stay chrome-side (.graph-full owns them).
  bg?: string;
  panel?: string;
  // Graph-node ink: non-centre discs wear it where the canvas graph drew --line-node outlines.
  lineNode?: string;
  fontSans?: string;
}

function readThemeColors(): ThemeColors {
  const styles = getComputedStyle(document.documentElement);
  const token = (name: string): string | undefined => styles.getPropertyValue(name).trim() || undefined;
  return {
    lineStrong: token("--line-strong"),
    mark: token("--mark"),
    text: token("--text"),
    bg: token("--bg"),
    panel: token("--panel"),
    lineNode: token("--line-node"),
    fontSans: token("--font-sans"),
  };
}

const FALLBACK_FONT = '"IBM Plex Sans JP", Inter, system-ui, sans-serif';

export function GraphOverviewSigma({ graph, onSelect }: GraphOverviewSigmaProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const rendererRef = useRef<Sigma<OverviewNodeAttributes, OverviewEdgeAttributes> | null>(null);
  // Reducers and label drawers read through this ref, so a theme swap repaints without rebuilding
  // the renderer or disturbing the camera.
  const colorsRef = useRef<ThemeColors>({});
  const onSelectRef = useRef(onSelect);
  onSelectRef.current = onSelect;

  const themeVersion = useThemeVersion();

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    colorsRef.current = readThemeColors();
    const colors = colorsRef.current;
    const graphologyGraph = graphToGraphology(graph);

    const settings: Partial<Settings<OverviewNodeAttributes, OverviewEdgeAttributes>> = {
      allowInvalidContainer: true,
      renderLabels: true,
      labelRenderedSizeThreshold: LABEL_SIZE_THRESHOLD,
      labelSize: 13,
      labelWeight: "500",
      labelFont: colors.fontSans || FALLBACK_FONT,
      nodeReducer: (node, data) => ({
        ...data,
        color:
          data.center || node === String(graph.center_id)
            ? colorsRef.current.mark
            : colorsRef.current.lineNode,
      }),
      edgeReducer: (_edge, data) => ({
        ...data,
        color: colorsRef.current.lineStrong,
      }),
      // sigma's stock hover chip hardcodes white ground and black ink, which breaks on the dark
      // theme; hover here is only a cursor plus the node's own label forced in ordinary ink.
      defaultDrawNodeHover: (context, data, drawerSettings) => {
        const ink = colorsRef.current.text;
        if (!ink || !data.label) return;
        context.fillStyle = ink;
        context.font = `${drawerSettings.labelWeight} ${drawerSettings.labelSize}px ${drawerSettings.labelFont}`;
        context.fillText(data.label, data.x + data.size + 3, data.y + drawerSettings.labelSize / 3);
      },
    };
    if (colors.text) settings.labelColor = { color: colors.text };

    const renderer = new Sigma(graphologyGraph, container, settings);
    rendererRef.current = renderer;

    renderer.on("clickNode", ({ node }) => onSelectRef.current(node));
    renderer.on("enterNode", () => {
      container.style.cursor = "pointer";
    });
    renderer.on("leaveNode", () => {
      container.style.cursor = "";
    });

    // Each worker "message" is one completed layout iteration, so the counter is the deterministic
    // cut-off: after LAYOUT_MAX_TICKS iterations the supervisor stops asking for more.
    let ticks = 0;
    let layout: FA2Layout | null = null;
    let worker: Worker | null = null;
    const onTick = () => {
      ticks += 1;
      if (ticks >= LAYOUT_MAX_TICKS) layout?.stop();
    };
    if (graphologyGraph.order > 1 && typeof Worker !== "undefined") {
      layout = new FA2Layout(graphologyGraph, {
        settings: forceAtlas2.inferSettings(graphologyGraph.order),
      });
      // The supervisor's d.ts omits the worker handle, but each of its "message" events is exactly
      // one completed iteration — the deterministic tick counter LAYOUT_MAX_TICKS cuts off.
      worker = (layout as unknown as { worker: Worker }).worker;
      worker.addEventListener("message", onTick);
      layout.start();
    }

    return () => {
      layout?.stop();
      worker?.removeEventListener("message", onTick);
      layout?.kill();
      renderer.kill();
      rendererRef.current = null;
    };
  }, [graph]);

  // Theme change: re-resolve the tokens and repaint. Reducers read through colorsRef, and the label
  // settings that cannot go through a reducer are re-set here; camera and layout are left alone.
  useEffect(() => {
    const renderer = rendererRef.current;
    if (!renderer) return;
    const colors = readThemeColors();
    colorsRef.current = colors;
    renderer.setSetting("labelFont", colors.fontSans || FALLBACK_FONT);
    if (colors.text) renderer.setSetting("labelColor", { color: colors.text });
    renderer.refresh();
  }, [themeVersion]);

  return (
    <>
      <div ref={containerRef} className="graph-overview" />
      <div className="graph-controls">
        <button
          className="graph-reset"
          type="button"
          aria-label="Reset graph view"
          title="Reset graph view"
          onClick={() => void rendererRef.current?.getCamera().animatedReset()}
        >
          <RailIcon Icon={IconRotate2} size={15} />
        </button>
      </div>
    </>
  );
}
