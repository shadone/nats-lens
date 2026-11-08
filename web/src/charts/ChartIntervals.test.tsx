import {Fragment,useState,type ComponentType,type ReactNode} from "react";
import {act,fireEvent,screen} from "@testing-library/react";
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {Link,useNavigate,useSearchParams} from "react-router";
import * as inspection from "./ChartInspection";
import {renderApp} from "../test/render";
// The pre-feature fallback keeps the missing interaction at runtime RED.
const Provider=(inspection as unknown as {ChartIntervalProvider?:ComponentType<{children:ReactNode}>}).ChartIntervalProvider??Fragment;
vi.mock("../api/systems",async()=>{const {useSearchParams}=await import("react-router");return {useSystems:()=>({data:{defaultSystem:"example-prod",systems:[{id:"example-prod"},{id:"example-staging"}]}}),useCurrentSystem:()=>{const [p]=useSearchParams();return {id:p.get("system")||"example-prod",known:true}}}});
const start=Date.parse("2026-10-17T13:00:00Z"),end=start+60_000;
const entity={kind:"stream" as const,key:"APP/orders",name:"orders",account:"APP",stream:"orders",consumer:"",server:""};
function Plot(){return <inspection.ChartInspection label="Storage" start={start} end={end} stepMs={1000} investigation={{metric:"stream.bytes",entity}} series={[{label:"bytes",points:[{time:start,text:"10 bytes"},{time:start+1000,text:"20 bytes"}]}]}><Link to="/streams/APP/orders">Plot details</Link><svg role="img" aria-label="Storage history"/></inspection.ChartInspection>}
function History(){const back=useNavigate();return <button onClick={()=>back(-1)}>Back</button>}
const route="/capacity?system=example-prod&account=APP&range=1h&epochA=123&epochB=456";
function mount(children:ReactNode=<Plot/>,url=route){return renderApp(<Provider><History/>{children}</Provider>,url)}
function drag(a=30,b=90,type="mouse"){
 const plot=screen.getByRole("slider",{name:"Inspect Storage"});vi.spyOn(plot,"getBoundingClientRect").mockReturnValue({left:10,width:100,top:0,height:40,right:110,bottom:40} as DOMRect);
 fireEvent.pointerDown(plot,{pointerId:1,isPrimary:true,button:0,clientX:a,clientY:20,pointerType:type});fireEvent.pointerMove(plot,{pointerId:1,clientX:b,clientY:20,pointerType:type});fireEvent.pointerUp(plot,{pointerId:1,button:0,clientX:b,clientY:20,pointerType:type});return plot;
}
afterEach(()=>vi.restoreAllMocks());beforeEach(()=>localStorage.clear());
it("highlights a dragged interval and waits for an explicit action",()=>{
 mount();drag();expect(screen.getByRole("dialog",{name:"Selected chart interval"})).toBeInTheDocument();expect(screen.getByRole("img",{name:/Selected chart interval/})).toBeInTheDocument();
 expect(Date.parse((screen.getByLabelText("Start (UTC)") as HTMLInputElement).value+"Z")).toBe(start+12000);expect(Date.parse((screen.getByLabelText("End (UTC)") as HTMLInputElement).value+"Z")).toBe(start+48000);expect(screen.getByTestId("location")).toHaveTextContent(route);
});
it("sets the exact global range and Back restores the previous live range",()=>{
 mount();drag();fireEvent.click(screen.getByRole("button",{name:"Set time range"}));const u=new URL(screen.getByTestId("location").textContent!,"http://example.com");
 expect(u.searchParams.get("range")).toBeNull();expect(Date.parse(u.searchParams.get("from")!)).toBe(start+12000);expect(Date.parse(u.searchParams.get("to")!)).toBe(start+48000);expect(u.searchParams.get("system")).toBe("example-prod");expect(u.searchParams.get("account")).toBe("APP");expect(u.searchParams.has("epochA")).toBe(false);
 fireEvent.click(screen.getByRole("button",{name:"Back"}));expect(screen.getByTestId("location")).toHaveTextContent(route);
});
it("normalizes reverse dragging and opens a scoped storage investigation",()=>{
 mount();drag(90,30);fireEvent.click(screen.getByRole("button",{name:"Investigate change"}));const u=new URL(screen.getByTestId("location").textContent!,"http://example.com");expect(u.pathname).toBe("/capacity/storage-change");expect(u.searchParams.get("system")).toBe("example-prod");expect(u.searchParams.get("account")).toBe("APP");expect(Date.parse(u.searchParams.get("from")!)).toBe(start+12000);expect(Date.parse(u.searchParams.get("to")!)).toBe(start+48000);expect(u.searchParams.has("epochA")).toBe(false);
});
it("cancels without changing range and suppresses the link click after a drag",()=>{
 mount();drag();fireEvent.click(screen.getByRole("link",{name:"Plot details"}));expect(screen.getByTestId("location")).toHaveTextContent(route);fireEvent(screen.getByRole("dialog"),new Event("cancel",{cancelable:true}));expect(screen.queryByRole("dialog")).toBeNull();expect(screen.getByTestId("location")).toHaveTextContent(route);
 drag(30,31);expect(screen.queryByRole("dialog")).toBeNull();fireEvent.click(screen.getByRole("link",{name:"Plot details"}));expect(screen.getByTestId("location")).toHaveTextContent("/streams/APP/orders");
});
it("offers touch/time-picker controls and preserves edited millisecond endpoints",()=>{
 mount();fireEvent.click(screen.getByRole("button",{name:"Select interval for Storage"}));fireEvent.change(screen.getByLabelText("Start (UTC)"),{target:{value:"2026-10-17T13:00:12.123"}});fireEvent.change(screen.getByLabelText("End (UTC)"),{target:{value:"2026-10-17T13:00:48.987"}});fireEvent.click(screen.getByRole("button",{name:"Set time range"}));const u=new URL(screen.getByTestId("location").textContent!,"http://example.com");expect(Date.parse(u.searchParams.get("from")!)).toBe(start+12123);expect(Date.parse(u.searchParams.get("to")!)).toBe(start+48987);
});
it("supports keyboard interval selection and rejects invalid picker values",()=>{
 mount();const p=screen.getByRole("slider",{name:"Inspect Storage"});act(()=>p.focus());fireEvent.keyDown(p,{key:"Home"});fireEvent.keyDown(p,{key:"ArrowRight",shiftKey:true});fireEvent.keyDown(p,{key:"ArrowRight",shiftKey:true});fireEvent.keyDown(p,{key:"Enter"});expect(Date.parse((screen.getByLabelText("End (UTC)") as HTMLInputElement).value+"Z")).toBe(start+2000);
 fireEvent.change(screen.getByLabelText("Start (UTC)"),{target:{value:"2026-10-17T14:00:00"}});expect(screen.getByRole("button",{name:"Set time range"})).toBeDisabled();expect(screen.getByRole("button",{name:"Investigate change"})).toBeDisabled();fireEvent.click(screen.getByRole("button",{name:"Cancel"}));expect(screen.queryByRole("dialog")).toBeNull();
});
it("keeps one selection dialog when a data refresh removes its chart",()=>{
 let hide!:()=>void;function Changing(){const [show,set]=useState(true);hide=()=>set(false);return show?<Plot/>:<p>Updated chart has no measurements</p>}
 const {container}=mount(<Changing/>);drag();act(()=>hide());expect(screen.getByRole("dialog",{name:"Selected chart interval"})).toBeInTheDocument();expect(Date.parse((screen.getByLabelText("Start (UTC)") as HTMLInputElement).value+"Z")).toBe(start+12000);expect(container.querySelectorAll("dialog")).toHaveLength(1);
});
it("dismisses stale selection when system scope changes",()=>{
 let change!:()=>void;function Change(){const [,set]=useSearchParams();change=()=>set({system:"example-staging",range:"1h"});return <Plot/>}
 mount(<Change/>);drag();expect(screen.getByRole("dialog")).toBeInTheDocument();act(()=>change());expect(screen.queryByRole("dialog")).toBeNull();expect(screen.queryByRole("img",{name:/Selected chart interval/})).toBeNull();
});
