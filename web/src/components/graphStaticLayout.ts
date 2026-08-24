import type { Graph, NoteID } from "../types";

export interface StaticPoint {
  x: number;
  y: number;
}

export interface StaticLayout {
  // One position per node, in world coordinates inside [0, width] x [0, height].
  positions: Map<NoteID, StaticPoint>;
  width: number;
  height: number;
}

// The overview layout replaces the force simulation for whole-vault graphs: a force pass over
// thousands of notes neither converges on time nor reads better than structure drawn straight from
// the link data. Each connected component becomes a radial tree — BFS from its best-connected node,
// one concentric ring per depth — and the components are shelved side by side like paragraphs.
// Everything is derived from note ids and link counts with no randomness and no iteration, so the
// same graph always produces pixel-identical output regardless of the order its arrays arrive in.

// World-space spacing between consecutive rings: enough room that a node's edges leave its ring
// visibly rather than tangling along it.
const RING_STEP = 96;
// Minimum arc between neighbouring nodes on the same ring. A level with many members grows its
// ring instead of crowding them, so a wide generation spreads before it starts to overlap.
const MIN_ARC = 26;
// Clear floor between packed components, so two clusters never read as one.
const COMPONENT_GAP = 90;

interface PlacedComponent {
  // Component-relative positions (bbox minimum subtracted), so packing is pure translation.
  points: Map<NoteID, StaticPoint>;
  width: number;
  height: number;
  key: string;
}

// layoutStatic lays the graph out deterministically in O(N log N + E): adjacency lists are sorted
// once, each node and edge is then visited a constant number of times, and the only sort beyond
// that orders the (far fewer) components onto shelves.
export function layoutStatic(graph: Graph): StaticLayout {
  const nodes = graph.nodes || [];
  if (nodes.length === 0) {
    return { positions: new Map(), width: 0, height: 0 };
  }

  const adjacency = new Map<NoteID, NoteID[]>();
  for (const node of nodes) adjacency.set(node.note_id, []);
  for (const edge of graph.edges || []) {
    const source = adjacency.get(edge.source_id);
    const target = adjacency.get(edge.target_id);
    if (!source || !target) continue; // an edge naming an unknown node draws nothing anywhere else either
    source.push(edge.target_id);
    target.push(edge.source_id);
  }
  // Sorted neighbours make traversal order depend on ids alone: the same graph yields the same
  // layout whichever order its nodes and edges arrived in.
  for (const neighbours of adjacency.values()) neighbours.sort();

  const visited = new Set<NoteID>();
  const components: PlacedComponent[] = [];
  for (const node of nodes) {
    if (visited.has(node.note_id)) continue;
    components.push(layoutComponent(collectComponent(node.note_id, adjacency, visited), adjacency));
  }

  return packComponents(components);
}

// collectComponent gathers one connected component's members. The walk order deliberately carries
// no information — the ring layout re-roots itself below — so membership is all that escapes here.
function collectComponent(
  seed: NoteID,
  adjacency: Map<NoteID, NoteID[]>,
  visited: Set<NoteID>,
): NoteID[] {
  const members: NoteID[] = [seed];
  visited.add(seed);
  let frontier = [seed];
  while (frontier.length > 0) {
    const next: NoteID[] = [];
    for (const id of frontier) {
      for (const neighbour of adjacency.get(id) || []) {
        if (visited.has(neighbour)) continue;
        visited.add(neighbour);
        members.push(neighbour);
        next.push(neighbour);
      }
    }
    frontier = next;
  }
  return members;
}

// layoutComponent rings one component: BFS from its best-connected node — ties going to the
// smallest id — produces one ring per depth, and siblings stay adjacent because discovery order
// keeps children near their parent's angle instead of scattering them around the circle.
function layoutComponent(members: NoteID[], adjacency: Map<NoteID, NoteID[]>): PlacedComponent {
  // The root must not depend on which member the component walk happened to enter through, so it
  // is chosen from the ids themselves: highest degree first, smallest id breaking ties.
  let root = members[0];
  let rootDegree = (adjacency.get(root) || []).length;
  for (const id of members) {
    const degree = (adjacency.get(id) || []).length;
    if (degree > rootDegree || (degree === rootDegree && id < root)) {
      root = id;
      rootDegree = degree;
    }
  }

  const levels: NoteID[][] = [[root]];
  const seen = new Set<NoteID>([root]);
  let frontier = [root];
  while (frontier.length > 0) {
    const next: NoteID[] = [];
    for (const id of frontier) {
      for (const neighbour of adjacency.get(id) || []) {
        if (seen.has(neighbour)) continue;
        seen.add(neighbour);
        next.push(neighbour);
      }
    }
    if (next.length > 0) levels.push(next);
    frontier = next;
  }

  // Ring radii grow by RING_STEP, except where a crowded generation needs a wider circle to keep
  // MIN_ARC between neighbours (arc = 2πr / count).
  const radii: number[] = [0];
  for (let depth = 1; depth < levels.length; depth++) {
    const needed = (levels[depth].length * MIN_ARC) / (Math.PI * 2);
    radii[depth] = Math.max(radii[depth - 1] + RING_STEP, needed);
  }

  let minX = Infinity;
  let maxX = -Infinity;
  let minY = Infinity;
  let maxY = -Infinity;
  const points = new Map<NoteID, StaticPoint>();
  for (let depth = 0; depth < levels.length; depth++) {
    const count = levels[depth].length;
    const radius = radii[depth];
    levels[depth].forEach((id, index) => {
      const angle = count === 1 ? 0 : -Math.PI / 2 + (Math.PI * 2 * index) / count;
      const x = Math.cos(angle) * radius;
      const y = Math.sin(angle) * radius;
      points.set(id, { x, y });
      minX = Math.min(minX, x);
      maxX = Math.max(maxX, x);
      minY = Math.min(minY, y);
      maxY = Math.max(maxY, y);
    });
  }

  // Shift to a non-negative bbox so shelf packing only ever translates components rightward/down.
  for (const [id, point] of points) {
    points.set(id, { x: point.x - minX, y: point.y - minY });
  }
  return { points, width: maxX - minX, height: maxY - minY, key: root };
}

// packComponents shelves the components left-to-right in rows: big ones first (they anchor the
// overview top-left), small ones filling in after. Rows over a fixed cell grid because one hub's
// footprint must not hand three thousand single-node components an empty square that size — at this
// vault's shape that difference is the one between a readable field and a blank canvas with a speck.
function packComponents(components: PlacedComponent[]): StaticLayout {
  components.sort(
    (a, b) => b.height - a.height || b.width - a.width || (a.key < b.key ? -1 : a.key > b.key ? 1 : 0),
  );

  const totalArea = components.reduce(
    (area, c) => area + (c.width + COMPONENT_GAP) * (c.height + COMPONENT_GAP),
    0,
  );
  const rowTarget = Math.sqrt(totalArea);

  const positions = new Map<NoteID, StaticPoint>();
  let x = 0;
  let y = 0;
  let rowHeight = 0;
  let width = 0;
  let height = 0;
  for (const component of components) {
    if (x > 0 && x + component.width > rowTarget) {
      x = 0;
      y += rowHeight + COMPONENT_GAP;
      rowHeight = 0;
    }
    for (const [id, point] of component.points) {
      positions.set(id, { x: point.x + x, y: point.y + y });
    }
    x += component.width + COMPONENT_GAP;
    rowHeight = Math.max(rowHeight, component.height);
    width = Math.max(width, x - COMPONENT_GAP);
    height = Math.max(height, y + rowHeight);
  }
  return { positions, width, height };
}
