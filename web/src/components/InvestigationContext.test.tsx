import {MetricBox} from "./MetricBox";
import {fireEvent,render,screen,waitFor} from "@testing-library/react";
import {MemoryRouter} from "react-router";
import {describe,it,expect,vi} from "vitest";
import {InvestigationButton,InvestigationProvider} from "./InvestigationContext";
import {ChartInspection} from "../charts/ChartInspection";
import {setServerNow} from "../lib/clock";
vi.mock("../api/systems",()=>({useCurrentSystem:()=>({id:"shop",known:true})}));
vi.mock("../api/queries",()=>({useLensInfo:()=>({data:{version:"fixture"}})}));
const now=Date.parse("2026-10-17T14:30:00Z");
function setup(){setServerNow(now,Date.now(),true);render(<MemoryRouter initialEntries={["/servers/nats-0?range=1h&account=APP&token=secret"]}><InvestigationProvider><InvestigationButton/><ChartInspection investigation={{metric:"server.cpu_percent"}} label="CPU" start={now-3600000} end={now} stepMs={60000} series={[]}><svg/></ChartInspection></InvestigationProvider></MemoryRouter>)}
function contextFrom(prompt:string){return JSON.parse(prompt.split("BEGIN NATS LENS CONTEXT JSON\n")[1].split("\nEND NATS LENS CONTEXT JSON")[0])}
describe("investigation handoff",()=>{
 it("copies a complete agent prompt in one explicit activation without a preview",async()=>{
  const write=vi.fn().mockResolvedValue(undefined);Object.defineProperty(navigator,"clipboard",{configurable:true,value:{writeText:write}});setup();
  expect(write).not.toHaveBeenCalled();
  fireEvent.click(screen.getAllByRole("button",{name:"Copy as prompt"})[0]);
  await waitFor(()=>expect(write).toHaveBeenCalledOnce());
  const prompt=write.mock.calls[0][0], c=contextFrom(prompt);
  expect(c.system).toBe("shop");expect(c.range.end).toBe("2026-10-17T14:30:00.000Z");expect(prompt).not.toContain("secret");
  expect(prompt).toContain("natslens investigate methods");expect(prompt).toContain("natslens investigate describe METHOD");
  expect(prompt).toContain("natslens investigate query METHOD -profiles FILE -profile NAME -context FILE");
  expect(prompt).toContain("GetDataHistory");expect(prompt).toContain("untrusted evidence");expect(prompt).toContain("localhost");
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();await screen.findByText("Prompt copied");
 });
 it("copies the keyboard-selected point and leaves a complete selectable prompt with retry on failure",async()=>{
  const write=vi.fn().mockRejectedValue(new Error("denied"));Object.defineProperty(navigator,"clipboard",{configurable:true,value:{writeText:write}});setup();
  const slider=screen.getByRole("slider",{name:"Inspect CPU"});fireEvent.focus(slider);fireEvent.keyDown(slider,{key:"ArrowRight"});fireEvent.keyDown(slider,{key:"c"});
  const text=await screen.findByLabelText("Agent prompt") as HTMLTextAreaElement;
  const c=contextFrom(text.value);expect(c.metric).toBe("server.cpu_percent");expect(c.point).toBe("2026-10-17T13:31:00.000Z");
  fireEvent.focus(text);expect(text.selectionEnd-text.selectionStart).toBe(text.value.length);
  write.mockResolvedValue(undefined);fireEvent.click(screen.getByRole("button",{name:"Retry copy"}));await waitFor(()=>expect(write).toHaveBeenCalledTimes(2));
  expect(write.mock.calls[1][0]).toBe(text.value);await screen.findByText("Prompt copied");
 });
 it("retains the selected time after focus moves to the chart action",async()=>{
  const write=vi.fn().mockResolvedValue(undefined);Object.defineProperty(navigator,"clipboard",{configurable:true,value:{writeText:write}});setup();
  const slider=screen.getByRole("slider",{name:"Inspect CPU"});fireEvent.focus(slider);fireEvent.keyDown(slider,{key:"ArrowRight"});fireEvent.blur(slider);
  fireEvent.click(screen.getAllByRole("button",{name:"Copy as prompt"})[1]);await waitFor(()=>expect(write).toHaveBeenCalledOnce());
  expect(contextFrom(write.mock.calls[0][0]).point).toBe("2026-10-17T13:31:00.000Z");
 });
 it("preserves known metric and entity IDs from a server metric box",async()=>{
  const write=vi.fn().mockResolvedValue(undefined);Object.defineProperty(navigator,"clipboard",{configurable:true,value:{writeText:write}});setServerNow(now,Date.now(),true);
  render(<MemoryRouter initialEntries={["/servers/nats-0?range=1h"]}><InvestigationProvider><MetricBox title="CPU" description="CPU percent" fmt={String} start={now-3600000} end={now} series={{metric:"server.cpu_percent",entity:{kind:"server",key:"nats-0",name:"nats-0",server:"nats-0",account:"",stream:"",consumer:""},unit:"percent",stepMs:60000,points:[{time:now-60000,value:42}]}} /></InvestigationProvider></MemoryRouter>);
  fireEvent.click(screen.getByRole("button",{name:"Copy as prompt"}));await waitFor(()=>expect(write).toHaveBeenCalledOnce());
  const c=contextFrom(write.mock.calls[0][0]);expect(c.metric).toBe("server.cpu_percent");expect(c.entity).toMatchObject({type:"ENTITY_TYPE_SERVER",key:"nats-0"});
 });
});
