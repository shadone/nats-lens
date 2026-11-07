import { screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { renderApp } from "../test/render";
import { HistoryContext } from "../components/HistoryNote";
import { SeriesChart } from "./SeriesChart";
import { TrendChart } from "../components/TrendChart";
import * as api from "../api/requests";
import { NOW, gap, staging } from "../test/fixtures/history";
vi.mock("../api/requests",()=>({getDataHistory:vi.fn()}));
const read=vi.mocked(api.getDataHistory), H=3600000;
beforeEach(()=>read.mockReset());
function Charts({start=NOW-H,end=NOW,trend=false}) {return <HistoryContext.Provider value={{start:NOW-24*H,end:NOW,live:true}}>{trend?<TrendChart title="Messages" start={start} end={end} layers={[]}/>:<SeriesChart title="CPU" start={start} end={end}/>}</HistoryContext.Provider>}
it.each([false,true])("notices actual omitted collection gaps outside SVG text (trend %s)",async(trend)=>{
 read.mockResolvedValue({...staging(),rangeStart:NOW-H,rangeEnd:NOW,gaps:Array.from({length:200},(_,i)=>gap({start:NOW-(i+1)*1000,end:NOW-i*1000-500})),gapsTotal:243});
 const r=renderApp(<Charts trend={trend}/>,"/?range=24h");
 const notice=await screen.findByRole("note",{name:/Collection gap limit/});
 expect(notice).toHaveTextContent("43 older collection gaps omitted");
 expect(notice.closest("svg")).toBeNull();
 expect(read).toHaveBeenCalledTimes(1);
 expect(read.mock.calls[0][1]).toEqual({kind:"live",preset:"1h"});
 r.unmount();
});
it.each([0,200])("does not warn from a full list alone or event-only truncation (collection total %s)",async(total)=>{
 read.mockResolvedValue({...staging(),rangeStart:NOW-H,rangeEnd:NOW,gaps:Array.from({length:200},()=>gap({kind:total===0?"eventsNotCaptured":"noCollections",start:NOW-30000,end:NOW-1000})),gapsTotal:total,listedGapsTotal:900});
 renderApp(<Charts/>,"/?range=24h");
 await waitFor(()=>expect(read).toHaveBeenCalledTimes(1));
 await waitFor(()=>expect(screen.queryByText("History gaps loading")).not.toBeInTheDocument());
 expect(screen.queryByRole("note",{name:/Collection gap limit/})).not.toBeInTheDocument();
});
it("queries a non-preset chart's exact domain so omissions outside it do not produce a notice",async()=>{
 read.mockImplementation(async(_sys,spec)=>({...staging(),rangeStart:NOW-H/2,rangeEnd:NOW,gaps:[],gapsTotal:spec?.kind==="fixed"?0:243}));
 renderApp(<Charts start={NOW-H/2}/>,"/?range=24h");
 await waitFor(()=>expect(read).toHaveBeenCalledTimes(1));
 expect(read.mock.calls[0][1]).toEqual({kind:"fixed",from:NOW-H/2,to:NOW});
 expect(screen.queryByRole("note",{name:/Collection gap limit/})).not.toBeInTheDocument();
});

it("does not warn when retained omission evidence is outside the moving chart",async()=>{
 read.mockResolvedValue({...staging(),rangeStart:NOW-H,rangeEnd:NOW,gaps:[],gapsTotal:43,newestOmittedCollectionGap:gap({start:NOW-H,end:NOW-H+1000})});
 renderApp(<Charts start={NOW-H+2000} end={NOW+2000}/>,"/?range=24h");
 await waitFor(()=>expect(read).toHaveBeenCalledTimes(1));
 await waitFor(()=>expect(screen.queryByText("History gaps loading")).not.toBeInTheDocument());
 expect(screen.queryByRole("note",{name:/Collection gap limit/})).not.toBeInTheDocument();
});
it("reads an exact fixed chart domain even inside a larger selected range",async()=>{
 read.mockResolvedValue({...staging(),rangeStart:NOW-H,rangeEnd:NOW,gaps:[],gapsTotal:0});
 renderApp(<Charts/>,`/?from=${new Date(NOW-24*H).toISOString()}&to=${new Date(NOW).toISOString()}`);
 await waitFor(()=>expect(read).toHaveBeenCalledTimes(1));
 expect(read.mock.calls[0][1]).toEqual({kind:"fixed",from:NOW-H,to:NOW});
});
it.each([false,true])("tells the reader history gaps are loading in HTML text outside the stretched SVG (trend %s)",async(trend)=>{
 let answer!:(v:ReturnType<typeof staging>)=>void;
 read.mockImplementation(()=>new Promise(yes=>{answer=yes}));
 const r=renderApp(<Charts trend={trend}/>,"/?range=24h");
 const status=await screen.findByRole("status");
 expect(status).toHaveTextContent("History gaps loading");
 expect(status.closest("svg")).toBeNull();
 expect(status.namespaceURI).toBe("http://www.w3.org/1999/xhtml");
 answer({...staging(),rangeStart:NOW-H,rangeEnd:NOW,gaps:[]});
 await waitFor(()=>expect(screen.queryByRole("status")).not.toBeInTheDocument());
 // beforeEach returns the mock, and Vitest calls it after the test as a teardown.
 read.mockReset();
 r.unmount();
});
