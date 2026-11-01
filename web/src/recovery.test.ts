import { afterEach, expect, it, vi } from "vitest";
import { reportRecoverable, showBootFailure } from "./recovery";
afterEach(()=>{vi.restoreAllMocks();document.body.innerHTML="";});
it("logs repeated optional failures once and preserves first-paint content after boot failure",()=>{
 const log=vi.spyOn(console,"warn").mockImplementation(()=>{});
 reportRecoverable("synthetic feature","failure");reportRecoverable("synthetic feature","failure");
 expect(log).toHaveBeenCalledTimes(1);
 document.body.innerHTML='<div id="root"><p>Readable first paint</p></div>';
 showBootFailure(new Error("synthetic boot failure"));showBootFailure(new Error("synthetic boot failure"));
 expect(document.body.textContent).toContain("Readable first paint");
 expect(document.querySelectorAll("#track-boot-recovery")).toHaveLength(1);
 expect(document.body.textContent).toContain("Reload application");
});
