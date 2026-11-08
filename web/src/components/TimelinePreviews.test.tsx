import {fireEvent, render, screen, within} from "@testing-library/react";
import {MemoryRouter} from "react-router";
import {expect, it} from "vitest";
import type {Timeline} from "../api/model";
import {StoryLanes} from "./StoryLanes";
import {Cells} from "./StatusTimeline";

const start = Date.parse("2026-10-17T23:59:00Z");
const timeline: Timeline = {
  start, bucketMs: 60_000, bucketCount: 2,
  rows: [{key:"consumer-006",checkCode:"consumer-006",checkName:"No delivery while messages wait",severity:"warning",cells:["warning","ok"],now:"ok"}],
  kubernetes: [{id:"k1",time:start+5000,source:"kubernetes",kind:"k8s.pod.restarted",shortLabel:"Restart",label:"nats-0 restarted"}],
  nats: [{id:"e1",time:start+10_000,source:"nats",kind:"client.disconnect",shortLabel:"Disconnect",label:"Worker connection closed",severity:"warning",eventId:"42"}],
  windows: [{start,end:start+90_000,label:"Rolling restart"}],
  natsTruncated:false,findingTruncated:false,kubernetesTruncated:false,
};

it("explains event ticks on hover and keyboard focus in a viewport overlay",()=>{
  render(<MemoryRouter><StoryLanes tl={timeline} end={start+120_000} live={false}/></MemoryRouter>);
  const mark=screen.getByRole("button",{name:/NATS event: Worker connection closed/});
  fireEvent.pointerEnter(mark);
  const tip=screen.getByRole("tooltip");
  expect(tip.parentElement).toBe(document.body);
  expect(tip).toHaveTextContent("client.disconnect");
  expect(tip).toHaveTextContent("2026-10-17 23:59:10 UTC");
  expect(tip).toHaveTextContent("42");
  fireEvent.pointerLeave(mark);
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  fireEvent.focus(mark);
  expect(screen.getByRole("tooltip")).toHaveTextContent("Worker connection closed");
  fireEvent.keyDown(mark,{key:"Escape"});
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
});

it("explains aggregate finding boxes and inferred windows without inventing exact onsets",()=>{
  render(<MemoryRouter><StoryLanes tl={timeline} end={start+120_000} live={false}/></MemoryRouter>);
  fireEvent.pointerEnter(screen.getByRole("button",{name:/Finding state: Warning/}));
  expect(screen.getByRole("tooltip")).toHaveTextContent("No delivery while messages wait");
  expect(screen.getByRole("tooltip")).toHaveTextContent(/bucket boundaries/i);
  fireEvent.pointerLeave(screen.getByRole("button",{name:/Finding state: Warning/}));
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
