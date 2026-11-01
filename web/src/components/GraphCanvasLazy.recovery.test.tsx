import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { GraphCanvas } from "./GraphCanvasLazy";
vi.mock("../hooks/useVisible",()=>({useVisible:()=>({ref:{current:null},visible:true})}));
vi.mock("./GraphCanvas",()=>{throw new Error("synthetic graph chunk failure")});
afterEach(()=>vi.restoreAllMocks());
it("keeps the body usable after a graph chunk rejection and supports explicit recovery",async()=>{
 vi.spyOn(console,"error").mockImplementation(()=>{});vi.spyOn(console,"warn").mockImplementation(()=>{});
 render(<><p>Readable body</p><GraphCanvas graph={{center_id:"",nodes:[],edges:[]}} onSelect={()=>{}} resetToken={0} /></>);
 expect(await screen.findByText(/Graph is unavailable/)).toBeTruthy();
 expect(screen.getByText("Readable body")).toBeTruthy();
 vi.doMock("./GraphCanvas",()=>({GraphCanvas:()=> <p>Recovered graph</p>}));
 fireEvent.click(screen.getByText("Retry graph"));
 expect(await screen.findByText("Recovered graph")).toBeTruthy();
});
