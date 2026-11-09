import {fireEvent, render, screen, within, waitFor} from "@testing-library/react";
import {MemoryRouter} from "react-router";
import {expect, it, vi} from "vitest";
import type {Timeline} from "../api/model";
import {StoryLanes} from "./StoryLanes";
import {Cells} from "./StatusTimeline";

vi.mock("../lib/useGlobalQuery",()=>({useGlobalQuery:()=>""}));
vi.mock("../api/findingEpisodes",()=>({useFindingEpisodes:()=>({data:{items:[],totalCount:0,nextPageToken:"",readEpoch:"0"},isPending:false,error:null})}));
vi.mock("../api/queries",()=>({useFindings:()=>({data:{items:[],totalCount:0,nextPageToken:""},isPending:false,error:null})}));

const start = Date.parse("2026-10-17T23:59:00Z");
const timeline: Timeline = {
  start, bucketMs: 60_000, bucketCount: 2,
  rows: [{key:"consumer-006",checkCode:"consumer-006",checkName:"No delivery while messages wait",severity:"warning",cells:["warning","ok"],now:"ok"}],
  kubernetes: [{id:"k1",time:start+5000,source:"kubernetes",kind:"k8s.pod.restarted",shortLabel:"Restart",label:"nats-0 restarted"}],
  nats: [{id:"e1",time:start+10_000,source:"nats",kind:"client.disconnect",shortLabel:"Disconnect",label:"Worker connection closed",severity:"warning",eventId:"42"}],
  windows: [{start,end:start+90_000,label:"Rolling restart",detail:"Observed restart"}],
  natsTruncated:false,findingTruncated:false,kubernetesTruncated:false,
};

it("explains event ticks on hover and keyboard focus in a viewport overlay",async()=>{
  render(<MemoryRouter><StoryLanes tl={timeline} end={start+120_000} live={false}/></MemoryRouter>);
  const mark=screen.getByRole("button",{name:/NATS event: Worker connection closed/});
  fireEvent.pointerEnter(mark);
  const tip=screen.getByRole("tooltip");
  expect(tip.parentElement).toBe(document.body);
  expect(tip).toHaveTextContent("client.disconnect");
  expect(tip).toHaveTextContent("2026-10-17 23:59:10 UTC");
  expect(tip).toHaveTextContent("42");
  fireEvent.pointerLeave(mark);
  await waitFor(()=>expect(screen.queryByRole("tooltip")).not.toBeInTheDocument());
  fireEvent.focus(mark);
  expect(screen.getByRole("tooltip")).toHaveTextContent("Worker connection closed");
  fireEvent.keyDown(mark,{key:"Escape"});
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
});

it("explains aggregate finding boxes and inferred windows without inventing exact onsets",async()=>{
  render(<MemoryRouter><StoryLanes tl={timeline} end={start+120_000} live={false}/></MemoryRouter>);
  fireEvent.pointerEnter(screen.getByRole("button",{name:/Finding state: Warning/}));
  expect(screen.getByRole("tooltip")).toHaveTextContent("No delivery while messages wait");
  expect(screen.getByRole("tooltip")).toHaveTextContent(/bucket boundaries/i);
  fireEvent.pointerLeave(screen.getByRole("button",{name:/Finding state: Warning/}));
  await waitFor(()=>expect(screen.queryByRole("tooltip")).not.toBeInTheDocument());
  fireEvent.pointerEnter(screen.getByRole("button",{name:/Window: Rolling restart/}));
  expect(screen.getByRole("tooltip")).toHaveTextContent("2026-10-18 00:00:30 UTC");
  expect(screen.getByRole("tooltip")).toHaveTextContent(/inferred/i);
});

it("explains expanded per-check buckets and preserves their historical versus current state",()=>{
  render(<Cells row={timeline.rows[0]} start={start} bucketMs={60_000}/>);
  const mark=screen.getByRole("button",{name:/consumer-006.*Warning/});
  fireEvent.focus(mark);
  const tip=screen.getByRole("tooltip");
  expect(within(tip).getByText(/No delivery while messages wait/)).toBeInTheDocument();
  expect(tip).toHaveTextContent("Current check state: OK");
  expect(tip).toHaveTextContent("2026-10-18 00:00:00 UTC");
});

it("opens persistent event details with an exact-event link and closes back to its mark",()=>{
  render(<MemoryRouter><StoryLanes tl={timeline} end={start+120_000} live={false}/></MemoryRouter>);
  const mark=screen.getByRole("button",{name:/NATS event: Worker connection closed/});
  mark.focus(); fireEvent.click(mark); fireEvent.pointerLeave(mark);
  const dialog=screen.getByRole("dialog",{name:/Worker connection closed/});
  expect(dialog).toHaveTextContent("client.disconnect");
  expect(within(dialog).getByRole("link",{name:"Open recorded event"})).toHaveAttribute("href",expect.stringContaining("id=42"));
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  fireEvent.click(within(dialog).getByRole("button",{name:"Close"}));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(mark).toHaveFocus();
});

it("opens window explanation and per-check evidence without claiming an exact incident",()=>{
  const {unmount}=render(<MemoryRouter><StoryLanes tl={timeline} end={start+120_000} live={false}/></MemoryRouter>);
  fireEvent.click(screen.getByRole("button",{name:/Window: Rolling restart/}));
  expect(screen.getByRole("dialog")).toHaveTextContent("Observed restart");
  unmount();
  render(<MemoryRouter><Cells row={timeline.rows[0]} start={start} bucketMs={60_000}/></MemoryRouter>);
  fireEvent.keyDown(screen.getByRole("button",{name:/consumer-006.*Warning/}),{key:"Enter"});
  expect(screen.getByRole("dialog")).toHaveTextContent("Current check state: OK");
  expect(within(screen.getByRole("dialog")).getByRole("link",{name:"Read check reference"})).toHaveAttribute("href","/checks/consumer-006");
});

it("keeps a selected finding interval open when refresh replaces its mark",()=>{
 const {rerender}=render(<MemoryRouter><StoryLanes tl={timeline} end={start+120_000} live/></MemoryRouter>);
 fireEvent.click(screen.getByRole("button",{name:/Finding state: Warning/}));
 expect(screen.getByRole("dialog")).toBeInTheDocument();
 const refreshed={...timeline,start:start+60_000,rows:[{...timeline.rows[0],cells:["ok","ok"] as const}],nats:[]};
 rerender(<MemoryRouter><StoryLanes tl={{...refreshed,rows:refreshed.rows.map(r=>({...r,cells:[...r.cells]}))}} end={start+180_000} live/></MemoryRouter>);
 expect(screen.getByRole("dialog")).toHaveTextContent("2026-10-17 23:59:00 UTC");
 expect(screen.getByRole("dialog")).toHaveClass("dlg-fullscreen");
});
