// Synthetic diagrams only. This probe runs unchanged in Chromium and real WKWebView.
import { fitDrawioViewport } from "/viewport.js";

const assert = (value, message) => { if (!value) throw new Error(message); };
const settle = () => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
const xml = (width, style = "", x = 0) => `<mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/>
<mxCell id="2" value="Synthetic figure" style="html=1;${style}" vertex="1" parent="1"><mxGeometry x="${x}" y="20" width="${width}" height="160" as="geometry"/></mxCell>
</root></mxGraphModel>`;

async function figure(source, hidden = false) {
  const frame = document.createElement("div");
  frame.className = "drawio-diagram";
  frame.style.width = "880px";
  if (hidden) frame.style.display = "none";
  const host = document.createElement("div");
  host.style.width = "100%";
  frame.append(host);
  document.querySelector(".markdown-view").append(frame);
  host.dataset.mxgraph = JSON.stringify({xml: source, page: 0, nav: false, toolbar: null,
    resize: false, "auto-fit": false, "allow-zoom-out": false});
  let viewer, dispose;
  GraphViewer.createViewerForElement(host, instance => {
    viewer = instance;
    dispose = fitDrawioViewport(host, instance);
  });
  if (hidden) frame.style.display = "block";
  for (let i = 0; i < 180 && !host.querySelector("svg"); i++) await settle();
  assert(host.querySelector("svg"), "viewer initialized");
  await settle();
  return {frame, host, viewer, dispose};
}

function verify(f, label) {
  const svg = f.host.querySelector("svg");
  const parent = f.frame.getBoundingClientRect(), box = svg.getBoundingClientRect();
  assert(f.frame.scrollWidth <= f.frame.clientWidth + 1, `${label}: frame overflow`);
  assert(f.host.scrollWidth <= f.host.clientWidth + 1, `${label}: host overflow`);
  assert(box.width <= parent.width + 0.5, `${label}: SVG fits parent`);
  const graph = f.viewer.graph, bounds = graph.getGraphBounds(), view = svg.viewBox.baseVal;
  assert(graph.view.scale === 1, `${label}: vendor fit does not mutate source scale`);
  assert(view.x <= bounds.x && view.y <= bounds.y &&
    view.x + view.width >= bounds.x + bounds.width &&
    view.y + view.height >= bounds.y + bounds.height, `${label}: complete renderer bounds`);
  const ratio = box.width / box.height;
  assert(Math.abs(ratio - view.width / view.height) < 0.03, `${label}: aspect ratio`);
  // Check actual rendered shape extents, including its stroke, against the screen viewport.
  for (const cell of Object.values(graph.model.cells)) {
    const state = graph.view.getState(cell);
    const b = state?.shape?.boundingBox;
    if (!b) continue;
    const matrix = graph.view.getDrawPane().getScreenCTM();
    const start = new DOMPoint(b.x, b.y).matrixTransform(matrix);
    const end = new DOMPoint(b.x + b.width, b.y + b.height).matrixTransform(matrix);
    assert(start.x >= box.left - 1 && start.y >= box.top - 1 &&
      end.x <= box.right + 1 && end.y <= box.bottom + 1, `${label}: shape not clipped`);
  }
  return box.width;
}

window.probe = (async () => {
  const large = await figure(xml(1400, "strokeWidth=12;rounded=1;", -90));
  for (const width of [880, 640, 840, 320.5, 880]) {
    large.frame.style.width = `${width}px`;
    await settle();
    verify(large, `parent ${width}`);
  }
  const small = await figure(xml(160));
  assert(verify(small, "small") < 180, "small figure is not enlarged");
  const hidden = await figure(xml(1200), true);
  verify(hidden, "initially hidden");
  // A later renderer size event (e.g. image/label completion) updates the viewport too.
  const model = large.viewer.graph.model;
  const geometry = model.getGeometry(model.getCell("2")).clone();
  geometry.width = 1800;
  model.setGeometry(model.getCell("2"), geometry);
  await settle();
  verify(large, "late model layout");
  verify(small, "independent second figure");
  for (const f of [large, small, hidden]) {
    f.dispose();
    assert(!f.host.resizedAttached, "resize sensor released");
    f.frame.remove();
  }
  return "PASS: parent resize, fractional width, complete bounds, small/hidden/multiple figures, late layout, cleanup";
})();
