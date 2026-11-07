import { useState } from "react";
import { act, screen, waitFor, fireEvent } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { HistoryContext, ServerChartHistory } from "./HistoryNote";
import { HistoryGaps } from "../charts/HistoryGaps";
import { MetricBox } from "./MetricBox";
import type { Series } from "../api/model";
import { renderApp } from "../test/render";
import * as api from "../api/requests";
import { NOW, staging } from "../test/fixtures/history";
vi.mock("../api/requests", () => ({ getDataHistory: vi.fn() }));
const read=vi.mocked(api.getDataHistory);
beforeEach(()=>read.mockReset());
const page={start:NOW-3600000,end:NOW,live:true};
function Charts({server="nats-0"}) {return <HistoryContext.Provider value={page}><ServerChartHistory server={server}><svg>{[0,1,2,3,4].map(i=><HistoryGaps key={i} start={page.start} end={NOW} width={200} height={40}/>)}</svg></ServerChartHistory></HistoryContext.Provider>}
it("shares one history request across all five charts, including server scope",async()=>{
 read.mockResolvedValue({...staging(),rangeStart:page.start,rangeEnd:NOW,gaps:[],serverGaps:[{start:NOW-60000,end:NOW,reason:"Server did not answer"}]});
 renderApp(<Charts/>,"/?range=1h&system=example-prod");
 await waitFor(()=>expect(screen.getAllByRole("img",{name:"Server did not answer"})).toHaveLength(5));
 expect(read).toHaveBeenCalledTimes(1); expect(read.mock.calls[0][3]).toBe("nats-0"); expect(read.mock.calls[0][0]).toBe("example-prod");
});
it("shows pending and error without inheriting another server's gaps",async()=>{
 read.mockResolvedValueOnce({...staging(),rangeStart:page.start,rangeEnd:NOW,gaps:[],serverGaps:[{start:NOW-60000,end:NOW,reason:"Server did not answer"}]});
 let reject!:(e:Error)=>void;read.mockImplementationOnce(()=>new Promise((_yes,no)=>{reject=no}));
 function Switch(){const [name,setName]=useState("nats-0");return <><button onClick={()=>setName("nats-1")}>Switch server</button><Charts server={name}/></>}
 renderApp(<Switch/>);
 await waitFor(()=>expect(screen.getAllByRole("img",{name:"Server did not answer"})).toHaveLength(5));
 // Keep the router and query client mounted during the entity change.

 fireEvent.click(screen.getByRole("button",{name:"Switch server"}));
 expect(screen.queryByRole("img",{name:"Server did not answer"})).not.toBeInTheDocument();
 expect(screen.getByRole("status")).toHaveTextContent("History gaps loading");
 await act(async()=>reject(new Error("unavailable")));
 expect(await screen.findByRole("status")).toHaveTextContent("History gaps unavailable");
});
it("never says a server's tile is steady while the server's history read fails",async()=>{
 read.mockRejectedValue(new Error("unavailable"));
 const series:Series={metric:"m",unit:"count",stepMs:1800000,points:[2,2,2].map((value,i)=>({time:NOW-3600000+i*1800000,value})),min:2,max:2};
 renderApp(<HistoryContext.Provider value={page}><ServerChartHistory server="nats-0"><MetricBox title="Conns" description="d" fmt={String} series={series} start={page.start} end={NOW}/></ServerChartHistory></HistoryContext.Provider>,"/servers/nats-0?range=1h&system=example-prod");
 await screen.findByText("History gaps unavailable",{selector:"p.dh-note"});
 await act(async()=>{await new Promise(resolve=>setTimeout(resolve,20));});
 const row=document.querySelector(".mb-track-note")!;
 expect(row.querySelector(".dh-chart-status")).toHaveTextContent("History gaps unavailable");
 expect(row.querySelector(".dh-status-keep")).not.toBeNull();
 // beforeEach returns the mock, and Vitest calls it after the test as a teardown.
 read.mockReset();
});
