import {act, fireEvent, render, screen} from "@testing-library/react";
import {MemoryRouter, useNavigate} from "react-router";
import {beforeEach, expect, it, vi} from "vitest";
import {InvestigationButton, InvestigationProvider} from "./InvestigationContext";
import {ChartInspection} from "../charts/ChartInspection";
import {setServerNow} from "../lib/clock";

const system = vi.hoisted(() => ({id:"shop", known:true}));
vi.mock("../api/systems", () => ({useCurrentSystem:()=>system}));
vi.mock("../api/queries", () => ({useLensInfo:()=>({data:{version:"fixture"}})}));
const now=Date.parse("2026-10-17T14:30:00Z");
function deferred(){let resolve!:()=>void, reject!:(e:Error)=>void;const promise=new Promise<void>((a,b)=>{resolve=a;reject=b});return {promise,resolve,reject}}
const fail=(d:ReturnType<typeof deferred>)=>act(async()=>d.reject(new Error("denied")));
const succeed=(d:ReturnType<typeof deferred>)=>act(async()=>d.resolve());
function context(prompt:string){return JSON.parse(prompt.split("BEGIN NATS LENS CONTEXT JSON\n")[1].split("\nEND NATS LENS CONTEXT JSON")[0])}
function Controls(){const navigate=useNavigate();return <><button onClick={()=>navigate("/events?system=shop&range=1h&account=APP")}>Navigate</button><button onClick={()=>navigate("/servers/nats-0?system=other&range=1h&account=APP")}>System</button><button onClick={()=>navigate("/servers/nats-0?system=shop&range=6h&account=APP")}>Range</button><button onClick={()=>navigate("/servers/nats-0?system=shop&range=1h&account=OTHER")}>Account</button><button onClick={()=>navigate("/servers/nats-0?system=shop&range=1h&account=APP&name=worker")}>Filter</button></>}
function tree(metric="server.cpu_percent") {return <MemoryRouter initialEntries={["/servers/nats-0?system=shop&range=1h&account=APP"]}><InvestigationProvider><Controls/><InvestigationButton/><ChartInspection label="CPU" start={now-3600000} end={now} stepMs={60000} series={[]} investigation={{metric,series:[{label:"CPU",metric},{label:"Memory",metric:"server.mem_bytes"}]}}><svg/></ChartInspection></InvestigationProvider></MemoryRouter>}
function setup(){const write=vi.fn<(text:string)=>Promise<void>>();Object.defineProperty(navigator,"clipboard",{configurable:true,value:{writeText:write}});return {write,...render(tree())}}
const page=()=>fireEvent.click(screen.getAllByRole("button",{name:"Copy as prompt"})[0]);
const chart=()=>fireEvent.click(screen.getAllByRole("button",{name:"Copy as prompt"})[1]);
const noFeedback=()=>{expect(screen.queryByRole("dialog")).not.toBeInTheDocument();expect(screen.queryByText("Prompt copied")).not.toBeInTheDocument()};
beforeEach(()=>{system.id="shop";system.known=true;setServerNow(now,Date.now(),true)});

// R1's reported sequence: page A, chart B, B succeeds, A fails late.
it("does not replace a newer successful copy with an older clipboard failure",async()=>{
 const {write}=setup();const old=deferred();write.mockImplementationOnce(()=>old.promise).mockResolvedValue(undefined);page();chart();await act(async()=>{});expect(screen.getByText("Prompt copied")).toBeInTheDocument();await fail(old);expect(screen.queryByRole("dialog")).not.toBeInTheDocument();expect(write).toHaveBeenCalledTimes(2);
});
it.each(["resolve","reject"] as const)("ignores an old %s while the newer action is pending",async outcome=>{
 const {write}=setup();const a=deferred(),b=deferred();write.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise);page();chart();await (outcome==="resolve"?succeed(a):fail(a));noFeedback();await succeed(b);expect(screen.getByText("Prompt copied")).toBeInTheDocument();
});
it.each(["resolve","reject"] as const)("preserves the latest complete chart fallback after an older %s, and retries exactly that frozen prompt",async outcome=>{
 const {write}=setup();const a=deferred(),b=deferred(),retry=deferred();write.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise).mockReturnValueOnce(retry.promise);
 page();const slider=screen.getByRole("slider");fireEvent.focus(slider);fireEvent.keyDown(slider,{key:"ArrowRight"});fireEvent.blur(slider);chart();await fail(b);
 const prompt=write.mock.calls[1][0];expect(context(prompt)).toMatchObject({metric:"server.cpu_percent",point:"2026-10-17T13:31:00.000Z",series:[{metric:"server.cpu_percent"},{metric:"server.mem_bytes"}]});
 await (outcome==="resolve"?succeed(a):fail(a));expect((screen.getByLabelText("Agent prompt") as HTMLTextAreaElement).value).toBe(prompt);expect(screen.queryByText("Prompt copied")).not.toBeInTheDocument();
 fireEvent.click(screen.getByRole("button",{name:"Retry copy"}));expect(write.mock.calls[2][0]).toBe(prompt);await succeed(retry);expect(screen.getByText("Prompt copied")).toBeInTheDocument();expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
it("invalidates an older retry when a new context is explicitly copied",async()=>{
 const {write}=setup();const a=deferred(),retry=deferred(),b=deferred();write.mockReturnValueOnce(a.promise).mockReturnValueOnce(retry.promise).mockReturnValueOnce(b.promise);page();await fail(a);fireEvent.click(screen.getByRole("button",{name:"Retry copy"}));fireEvent.click(screen.getByRole("button",{name:"Close"}));chart();await fail(b);const prompt=write.mock.calls[2][0];await succeed(retry);expect((screen.getByLabelText("Agent prompt") as HTMLTextAreaElement).value).toBe(prompt);expect(write).toHaveBeenCalledTimes(3);
});
it.each(["Navigate","System","Range","Account","Filter"])("discards pending completion and existing fallback after %s scope changes",async name=>{
 const {write}=setup();const a=deferred(),b=deferred();write.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise);page();if(name==="System")system.id="other";fireEvent.click(screen.getByRole("button",{name}));await fail(a);noFeedback();page();await fail(b);expect(screen.getByRole("dialog")).toBeInTheDocument();fireEvent.click(screen.getByRole("button",{name:name==="Navigate"?"Account":"Navigate"}));noFeedback();expect(write).toHaveBeenCalledTimes(2);
});
it("discards pending success on navigation without another clipboard write",async()=>{const {write}=setup();const a=deferred();write.mockReturnValue(a.promise);page();fireEvent.click(screen.getByRole("button",{name:"Navigate"}));await succeed(a);noFeedback();expect(write).toHaveBeenCalledOnce()});
it("invalidates chart completions and fallback after selected point or metric changes",async()=>{
 const {write,rerender}=setup();const a=deferred(),b=deferred();write.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise);chart();const slider=screen.getByRole("slider");fireEvent.focus(slider);fireEvent.keyDown(slider,{key:"ArrowRight"});await fail(a);noFeedback();chart();await fail(b);expect(screen.getByRole("dialog")).toBeInTheDocument();rerender(tree("server.other_metric"));noFeedback();expect(write).toHaveBeenCalledTimes(2);
});
it("keeps a pending action valid across equivalent rerenders and live-clock ticks",async()=>{
 const {write,rerender}=setup();const a=deferred();write.mockReturnValue(a.promise);chart();const prompt=write.mock.calls[0][0];setServerNow(now+30000,Date.now(),true);rerender(tree());await fail(a);expect((screen.getByLabelText("Agent prompt") as HTMLTextAreaElement).value).toBe(prompt);expect(context(prompt).range.end).toBe("2026-10-17T14:30:00.000Z");
});
it.each(["resolve","reject"] as const)("ignores %s after unmount, including when another provider mounts",async outcome=>{
 const {write,unmount}=setup();const a=deferred();write.mockReturnValue(a.promise);page();unmount();render(tree());await (outcome==="resolve"?succeed(a):fail(a));noFeedback();expect(write).toHaveBeenCalledOnce();
});
