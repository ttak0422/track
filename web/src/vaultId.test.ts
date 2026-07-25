import { describe, expect, it } from "vitest";
import { idParams, qualify, split, vaultOf } from "./vaultId";

describe("vault-qualified note ids", () => {
  it("leaves ids from an unnamed vault bare", () => {
    // A workspace serving one unregistered vault keeps the ids it always had, so its URLs and
    // stored tabs survive the move to qualified ids.
    expect(qualify("", 123)).toBe("123");
    expect(qualify(undefined, "slug")).toBe("slug");
    expect(split("123")).toEqual({ vault: "", id: "123" });
  });

  it("round-trips an id through its vault", () => {
    expect(qualify("work", 20260725)).toBe("work:20260725");
    expect(split("work:20260725")).toEqual({ vault: "work", id: "20260725" });
    expect(vaultOf("work:20260725")).toBe("work");
  });

  it("keeps two vaults' same-numbered notes distinct", () => {
    // Journal ids are the date, so every vault has one under the same number.
    expect(qualify("main", 20260725)).not.toBe(qualify("work", 20260725));
  });

  it("splits on the first colon so a slug may contain more", () => {
    expect(split("work:a:b")).toEqual({ vault: "work", id: "a:b" });
  });

  it("addresses a request at the vault the id names", () => {
    expect(idParams("work:7").toString()).toBe("id=7&vault=work");
    expect(idParams("7").toString()).toBe("id=7");
  });
});
