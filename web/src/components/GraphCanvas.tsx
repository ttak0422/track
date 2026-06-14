import { PointerEvent, WheelEvent, useEffect, useMemo, useRef, useState } from "react";
import type { Graph, GraphNode, NoteID } from "../types";

interface GraphCanvasProps {
  graph: Graph;
  onSelect: (noteID: NoteID) => void;
}

interface LayoutNode extends GraphNode {
  x: number;
  y: number;
  radius: number;
}

interface Viewport {
  x: number;
  y: number;
  scale: number;
}

interface Point {
  x: number;
  y: number;
}

export function GraphCanvas({ graph, onSelect }: GraphCanvasProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const pointerRef = useRef<{ id: number; start: Point; last: Point; moved: boolean } | null>(null);
  const [viewport, setViewport] = useState<Viewport>({ x: 0, y: 0, scale: 1 });
  const [size, setSize] = useState({ width: 1, height: 1 });
  const nodes = useMemo(() => layoutGraph(graph), [graph]);
  const nodeByID = useMemo(() => new Map(nodes.map((node) => [node.note_id, node])), [nodes]);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const observer = new ResizeObserver(([entry]) => {
      const rect = entry.contentRect;
      setSize({ width: Math.max(1, rect.width), height: Math.max(1, rect.height) });
    });
    observer.observe(canvas);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    setViewport({ x: 0, y: 0, scale: 1 });
  }, [graph]);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const ratio = window.devicePixelRatio || 1;
    canvas.width = Math.floor(size.width * ratio);
    canvas.height = Math.floor(size.height * ratio);
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
    draw(ctx, size, viewport, nodes, graph, nodeByID);
  }, [graph, nodeByID, nodes, size, viewport]);

  function canvasPoint(event: PointerEvent<HTMLCanvasElement>): Point {
    const rect = event.currentTarget.getBoundingClientRect();
    return { x: event.clientX - rect.left, y: event.clientY - rect.top };
  }

  function worldPoint(point: Point): Point {
    return {
      x: (point.x - size.width / 2 - viewport.x) / viewport.scale,
      y: (point.y - size.height / 2 - viewport.y) / viewport.scale,
    };
  }

  function nodeAt(point: Point): LayoutNode | undefined {
    const world = worldPoint(point);
    return nodes.find((node) => {
      const dx = world.x - node.x;
      const dy = world.y - node.y;
      return Math.hypot(dx, dy) <= node.radius + 5;
    });
  }

  function pointerDown(event: PointerEvent<HTMLCanvasElement>) {
    const point = canvasPoint(event);
    pointerRef.current = { id: event.pointerId, start: point, last: point, moved: false };
    event.currentTarget.setPointerCapture(event.pointerId);
  }

  function pointerMove(event: PointerEvent<HTMLCanvasElement>) {
    const pointer = pointerRef.current;
    if (!pointer || pointer.id !== event.pointerId) return;
    const point = canvasPoint(event);
    const dx = point.x - pointer.last.x;
    const dy = point.y - pointer.last.y;
    pointer.last = point;
    pointer.moved ||= Math.hypot(point.x - pointer.start.x, point.y - pointer.start.y) > 4;
    setViewport((current) => ({ ...current, x: current.x + dx, y: current.y + dy }));
  }

  function pointerUp(event: PointerEvent<HTMLCanvasElement>) {
    const pointer = pointerRef.current;
    if (!pointer || pointer.id !== event.pointerId) return;
    pointerRef.current = null;
    event.currentTarget.releasePointerCapture(event.pointerId);
    if (pointer.moved) return;
    const node = nodeAt(canvasPoint(event));
    if (node) onSelect(node.note_id);
  }

  function wheel(event: WheelEvent<HTMLCanvasElement>) {
    event.preventDefault();
    const nextScale = clamp(viewport.scale * (event.deltaY < 0 ? 1.12 : 0.88), 0.35, 3);
    setViewport((current) => ({ ...current, scale: nextScale }));
  }

  return (
    <canvas
      ref={canvasRef}
      className="graph-canvas"
      onPointerDown={pointerDown}
      onPointerMove={pointerMove}
      onPointerUp={pointerUp}
      onPointerCancel={() => {
        pointerRef.current = null;
      }}
      onWheel={wheel}
    />
  );
}

function layoutGraph(graph: Graph): LayoutNode[] {
  const degree = new Map<NoteID, number>();
  for (const edge of graph.edges) {
    degree.set(edge.source_id, (degree.get(edge.source_id) ?? 0) + 1);
    degree.set(edge.target_id, (degree.get(edge.target_id) ?? 0) + 1);
  }

  const radius = Math.max(90, graph.nodes.length * 12);
  return graph.nodes.map((node, index) => {
    const angle = (Math.PI * 2 * index) / Math.max(1, graph.nodes.length);
    const center = node.center || node.note_id === graph.center_id;
    return {
      ...node,
      x: center ? 0 : Math.cos(angle) * radius,
      y: center ? 0 : Math.sin(angle) * radius,
      radius: center ? 11 : 7 + Math.min(8, degree.get(node.note_id) ?? 0),
    };
  });
}

function draw(
  ctx: CanvasRenderingContext2D,
  size: { width: number; height: number },
  viewport: Viewport,
  nodes: LayoutNode[],
  graph: Graph,
  nodeByID: Map<NoteID, LayoutNode>,
) {
  ctx.clearRect(0, 0, size.width, size.height);
  ctx.save();
  ctx.translate(size.width / 2 + viewport.x, size.height / 2 + viewport.y);
  ctx.scale(viewport.scale, viewport.scale);
  ctx.lineWidth = 1 / viewport.scale;
  ctx.strokeStyle = css("--line");

  for (const edge of graph.edges) {
    const source = nodeByID.get(edge.source_id);
    const target = nodeByID.get(edge.target_id);
    if (!source || !target) continue;
    ctx.beginPath();
    ctx.moveTo(source.x, source.y);
    ctx.lineTo(target.x, target.y);
    ctx.stroke();
  }

  for (const node of nodes) {
    ctx.beginPath();
    ctx.fillStyle = node.center || node.note_id === graph.center_id ? css("--accent") : css("--panel");
    ctx.strokeStyle = node.generated_by_ai ? css("--danger") : css("--accent");
    ctx.lineWidth = 1.5 / viewport.scale;
    ctx.arc(node.x, node.y, node.radius, 0, Math.PI * 2);
    ctx.fill();
    ctx.stroke();

    if (viewport.scale > 0.7 || node.center || node.note_id === graph.center_id) {
      ctx.fillStyle = css("--text");
      ctx.font = `${12 / viewport.scale}px sans-serif`;
      ctx.textAlign = "center";
      ctx.fillText(trim(node.title, 22), node.x, node.y + node.radius + 14 / viewport.scale);
    }
  }

  ctx.restore();
}

function css(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

function trim(text: string, max: number): string {
  return text.length <= max ? text : `${text.slice(0, max - 1)}...`;
}
