import mermaid, { type Mermaid } from "mermaid";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { mermaidConfig } from "./MermaidDiagram";
import { registerMermaidIcons } from "./mermaidIcons";

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn(() => Promise.reject(new Error("Diagrams must not fetch external icons"))));
  mermaid.initialize(mermaidConfig());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("Mermaid architecture compatibility", () => {
  it("parses the help example with groups, local logos, ports, arrows, and alignment", async () => {
    const result = await mermaid.parse(`architecture-beta
  group backend(cloud)[Backend]
  service api(logos:aws-lambda)[API] in backend
  service db(logos:postgresql)[Database] in backend
  service cache(logos:redis)[Cache] in backend
  db:R --> L:api
  cache:R --> L:api
  align column db cache`);
    expect(result).toMatchObject({ diagramType: "architecture" });
    expect(fetch).not.toHaveBeenCalled();
  });

  it("supports the built-in icons and junctions without an icon pack", async () => {
    const result = await mermaid.parse(`architecture-beta
  group platform(cloud)[Platform]
  service web(internet)[Web] in platform
  service api(server)[API] in platform
  service db(database)[Database] in platform
  service files(disk)[Files] in platform
  junction traffic in platform
  web:R -- L:traffic
  traffic:R --> L:api
  api:B --> T:db
  traffic:B --> L:files`);
    expect(result).toMatchObject({ diagramType: "architecture" });
  });

  it("rejects missing services and recovers for a later valid diagram", async () => {
    await expect(mermaid.parse("architecture-beta\napi:R --> L:missing")).rejects.toThrow();
    await expect(mermaid.parse("architecture-beta\nservice api(server)[API]")).resolves.toMatchObject({
      diagramType: "architecture",
    });
  });

  it("does not let architecture frontmatter weaken strict security", async () => {
    await mermaid.parse(`---
config:
  securityLevel: loose
  secure: []
---
architecture-beta
  service api(server)[API]`);
    expect(mermaid.mermaidAPI.getConfig().securityLevel).toBe("strict");
    expect(fetch).not.toHaveBeenCalled();
  });

  it("registers the local logos loader once and resolves the documented icons without fetching", async () => {
    const registerIconPacks = vi.fn<Mermaid["registerIconPacks"]>();
    const renderer = { registerIconPacks } as unknown as Mermaid;
    registerMermaidIcons(renderer);
    registerMermaidIcons(renderer);
    expect(registerIconPacks).toHaveBeenCalledTimes(1);
    const [pack] = registerIconPacks.mock.calls[0][0];
    expect(pack.name).toBe("logos");
    expect("loader" in pack).toBe(true);
    if (!("loader" in pack)) throw new Error("Expected a lazy icon loader");
    const icons = await pack.loader();
    for (const name of ["aws-lambda", "postgresql", "redis"]) {
      expect(icons.icons[name]?.body).toContain("<path");
    }
    expect(fetch).not.toHaveBeenCalled();
  });
});
