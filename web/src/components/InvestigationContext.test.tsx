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
describe("investigation handoff",()=>{
 it("copies only after an explicit action and keeps the human question separate",async()=>{
  const write=vi.fn().mockResolvedValue(undefined);Object.defineProperty(navigator,"clipboard",{configurable:true,value:{writeText:write}});setup();
  fireEvent.click(screen.getByRole("button",{name:"Copy investigation context"}));
  expect(write).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText("Your question (optional)"),{target:{value:"What changed?"}});
  fireEvent.click(screen.getByRole("button",{name:"Copy context"}));await waitFor(()=>expect(write).toHaveBeenCalledOnce());
  const c=JSON.parse(write.mock.calls[0][0]);expect(c.annotation).toBe("What changed?");expect(c.system).toBe("shop");expect(c.range.end).toBe("2026-10-17T14:30:00.000Z");expect(JSON.stringify(c)).not.toContain("secret");
 });
 it("hands off the keyboard-selected chart point and offers selectable text when clipboard fails",async()=>{
  Object.defineProperty(navigator,"clipboard",{configurable:true,value:{writeText:vi.fn().mockRejectedValue(new Error("denied"))}});setup();
  const slider=screen.getByRole("slider",{name:"Inspect CPU"});fireEvent.focus(slider);fireEvent.keyDown(slider,{key:"ArrowRight"});fireEvent.keyDown(slider,{key:"c"});
  const text=screen.getByLabelText("Portable context") as HTMLTextAreaElement;
  const c=JSON.parse(text.value);expect(c.metric).toBe("server.cpu_percent");expect(c.point).toBe("2026-10-17T13:31:00.000Z");
  fireEvent.focus(text);expect(text.selectionEnd-text.selectionStart).toBe(text.value.length);
  fireEvent.click(screen.getByRole("button",{name:"Copy context"}));await screen.findByText(/Clipboard unavailable/);
 });
});
it("keeps the selected time when focus moves to the chart handoff button",()=>{
 setup();const slider=screen.getByRole("slider",{name:"Inspect CPU"});fireEvent.focus(slider);fireEvent.keyDown(slider,{key:"ArrowRight"});fireEvent.blur(slider);fireEvent.click(screen.getByRole("button",{name:"Investigate CPU"}));
 expect(JSON.parse((screen.getByLabelText("Portable context") as HTMLTextAreaElement).value).point).toBe("2026-10-17T13:31:00.000Z");
});

it("preserves known metric and entity IDs from a server metric box",()=>{
 setServerNow(now,Date.now(),true);
 render(<MemoryRouter initialEntries={["/servers/nats-0?range=1h"]}><InvestigationProvider><MetricBox title="CPU" description="CPU percent" fmt={String} start={now-3600000} end={now} series={{metric:"server.cpu_percent",entity:{kind:"server",key:"nats-0",name:"nats-0",server:"nats-0",account:"",stream:"",consumer:""},unit:"percent",stepMs:60000,points:[{time:now-60000,value:42}]}} /></InvestigationProvider></MemoryRouter>);
 fireEvent.click(screen.getByRole("button",{name:"Investigate CPU"}));
 const c=JSON.parse((screen.getByLabelText("Portable context") as HTMLTextAreaElement).value);
 expect(c.metric).toBe("server.cpu_percent");expect(c.entity).toMatchObject({type:"ENTITY_TYPE_SERVER",key:"nats-0"});
});
