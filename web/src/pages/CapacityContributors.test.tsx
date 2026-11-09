import {create,fromJson} from "@bufbuild/protobuf";
import {TimestampSchema} from "@bufbuild/protobuf/wkt";
import {fireEvent,screen,waitFor,within} from "@testing-library/react";
import {beforeEach,expect,it,vi} from "vitest";
import {useSearchParams} from "react-router";
import {lens} from "../api/client";
import {GetStorageChangeResponseSchema,StorageChangeOrder} from "../gen/natslens/v1/storage_change_pb";
import {CapacityStreamKind} from "../gen/natslens/v1/capacity_pb";
import {prod} from "../test/fixtures/capacity";
import {renderApp} from "../test/render";
import {Capacity} from "./Capacity";
vi.mock("../api/client",()=>({lens:{getStorageChange:vi.fn(),getLensInfo:vi.fn(),getTimeSeriesBatch:vi.fn()}}));
vi.mock("../api/requests",async()=>{const real=await vi.importActual<typeof import("../api/requests")>("../api/requests");return {...real,getCapacity:vi.fn(async()=>prod()),getLensInfo:vi.fn(async()=>({identityMode:"request",version:"dev",cluster:"c",collectIntervalMs:30000,retentionMs:0,live:true,demo:true}))}});
const start="2026-10-17T13:00:00Z",end="2026-10-17T13:05:00Z";
const stamp=(s:string)=>fromJson(TimestampSchema,s);
const route=`/capacity?system=example-prod&account=APP&from=${start}&to=${end}`;
function response(){return create(GetStorageChangeResponseSchema,{system:"example-prod",account:"APP",a:{epoch:9007199254740993n,requested:stamp(start),startedAt:stamp(start),finishedAt:stamp(start),inventoryComplete:true},b:{epoch:9007199254740994n,requested:stamp(end),startedAt:stamp(end),finishedAt:stamp(end),inventoryComplete:false,reasons:["missing_jsz"]},rows:[
 {id:"orders",rowKind:"resource",account:"APP",stream:"orders",kind:CapacityStreamKind.STREAM,storage:"file",before:{logical:0n},after:{logical:9007199254740993n},delta:{logical:9007199254740993n},configuredReplicasA:1,configuredReplicasB:3,processesA:["S0"],processesB:["S0","S1","S2"]},
 {id:"gone",rowKind:"resource",account:"APP",stream:"KV_old-cache",bucket:"old-cache",kind:CapacityStreamKind.KEY_VALUE,storage:"file",before:{logical:50n},after:{logical:0n},delta:{logical:-50n},reasons:["observed_delete_event"],eventIds:[9007199254740999n],processesA:["S0"]},
 {id:"new",rowKind:"resource",account:"APP",stream:"OBJ_images",bucket:"images",kind:CapacityStreamKind.OBJECT_STORE,storage:"file",before:{logical:0n},after:{logical:30n},delta:{logical:30n},reasons:["created_after_a"],processesB:["S1"]},
 {id:"unknown",rowKind:"resource",account:"APP",stream:"payments",kind:CapacityStreamKind.STREAM,storage:"file",before:{logical:9n},reasons:["not_observed_at_b"]}],totalCount:4,totals:[{storage:"file",knownContributions:{logical:9007199254740973n},unknownRows:1}]});}
const region=()=>within(screen.getByRole("region",{name:"Resources by measured change"}));
beforeEach(()=>{vi.clearAllMocks();vi.mocked(lens.getStorageChange).mockResolvedValue(response());vi.mocked(lens.getTimeSeriesBatch).mockResolvedValue({results:[]} as never)});
it("shows short-range signed changes for all resource types including resources absent at B",async()=>{
 renderApp(<Capacity/>,route);await region().findByRole("button",{name:"Sort by Change in range"});
 expect(region().getByRole("button",{name:"Sort by Change in range"})).toBeInTheDocument();
 expect(region().getByText("+9,007,199,254,740,993 bytes")).toBeInTheDocument();expect(region().getByText("−50 bytes")).toBeInTheDocument();
 expect(region().getByText("old-cache")).toBeInTheDocument();expect(region().getByText("images")).toBeInTheDocument();expect(region().getByText("payments").closest("tr")).toHaveTextContent("Unknown");
 expect(lens.getStorageChange).toHaveBeenCalledWith(expect.objectContaining({a:stamp(start),b:stamp(end),resourcesOnly:true,system:"example-prod",account:"APP"}),expect.anything());
 const link=new URL(region().getByRole("link",{name:"Open pinned investigation"}).getAttribute("href")!,"http://example.com");expect(link.searchParams.get("epochA")).toBe("9007199254740993");expect(link.searchParams.get("system")).toBe("example-prod");
});
it("requests signed sorting across the full ranking rather than sorting only a loaded page",async()=>{
 renderApp(<Capacity/>,route);await region().findByRole("button",{name:"Sort by Change in range"});
 fireEvent.click(region().getByRole("button",{name:"Sort by Change in range"}));await waitFor(()=>expect(lens.getStorageChange).toHaveBeenLastCalledWith(expect.objectContaining({order:StorageChangeOrder.LOGICAL_DESCENDING}),expect.anything()));
 fireEvent.click(await region().findByRole("button",{name:"Sort by Change in range"}));await waitFor(()=>expect(lens.getStorageChange).toHaveBeenLastCalledWith(expect.objectContaining({order:StorageChangeOrder.LOGICAL_ASCENDING}),expect.anything()));
 expect(screen.getByTestId("location")).toHaveTextContent("change=asc");
});
it("filters the complete resource ranking by type and retains account scope",async()=>{
 renderApp(<Capacity/>,route);await region().findByRole("button",{name:"Sort by Change in range"});
 fireEvent.change(region().getByLabelText("Contributor type"),{target:{value:"kv"}});
 await waitFor(()=>expect(lens.getStorageChange).toHaveBeenLastCalledWith(expect.objectContaining({resourceKind:CapacityStreamKind.KEY_VALUE,account:"APP",resourcesOnly:true,pageToken:""}),expect.anything()));
 expect(await region().findByText(/Totals cover all resource types/)).toBeInTheDocument();
});
it("continues one pinned page explicitly without automatically traversing every row",async()=>{
 const first=response();first.nextPageToken="page2";first.totalCount=105;vi.mocked(lens.getStorageChange).mockResolvedValue(first);
 renderApp(<Capacity/>,route);await region().findByRole("button",{name:"Sort by Change in range"});
 expect(lens.getStorageChange).toHaveBeenCalledTimes(1);fireEvent.click(region().getByRole("button",{name:"Next contributors"}));
 await waitFor(()=>expect(lens.getStorageChange).toHaveBeenLastCalledWith(expect.objectContaining({epochA:9007199254740993n,epochB:9007199254740994n,pageToken:"page2"}),expect.anything()));
});
it("offers the selected interval chart, endpoint configuration, events and consumers for a contributor",async()=>{
 renderApp(<Capacity/>,route);await region().findByRole("button",{name:"Sort by Change in range"});
 fireEvent.click(region().getByRole("button",{name:/^Explore orders/}));
 expect(await screen.findByRole("heading",{name:"orders evidence"})).toBeInTheDocument();expect(screen.getByText(/Configured replicas: 1 → 3/)).toBeInTheDocument();
 for(const name of ["Configuration at A","Configuration at B","Events in interval","Consumers in interval"]){const link=new URL(screen.getByRole("link",{name}).getAttribute("href")!,"http://example.com");expect(link.searchParams.get("system")).toBe("example-prod");expect(link.searchParams.get("account")).toBe("APP")}
 expect(screen.getByRole("img",{name:/^Logical bytes in interval/})).toBeInTheDocument();
});
it("does not keep a previous range's measurements when the scope changes",async()=>{
 function ChangeScope(){const [,set]=useSearchParams();return <button onClick={()=>set({system:"example-staging",from:start,to:end})}>Change scope</button>}
 renderApp(<><ChangeScope/><Capacity/></>,route);await region().findByRole("button",{name:"Sort by Change in range"});
 vi.mocked(lens.getStorageChange).mockImplementation(()=>new Promise(()=>{}));fireEvent.click(screen.getByRole("button",{name:"Change scope"}));
 await waitFor(()=>expect(screen.queryByText("+9,007,199,254,740,993 bytes")).not.toBeInTheDocument());
});
it("freezes the source clock and explicitly refreshes without showing a stale comparison",async()=>{
 const {GetLensInfoResponseSchema}=await import("../gen/natslens/v1/info_pb");const info=(s:string)=>create(GetLensInfoResponseSchema,{now:stamp(s),nowFixed:true});
 let finish!:(v:ReturnType<typeof info>)=>void;
 vi.mocked(lens.getLensInfo).mockResolvedValueOnce(info("2026-10-17T14:30:00Z")).mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 renderApp(<Capacity/>,"/capacity?system=example-prod&account=APP&range=1h");await region().findByRole("button",{name:"Sort by Change in range"});
 expect(lens.getStorageChange).toHaveBeenLastCalledWith(expect.objectContaining({a:stamp("2026-10-17T13:30:00Z"),b:stamp("2026-10-17T14:30:00Z")}),expect.anything());
 fireEvent.click(region().getByRole("button",{name:"Refresh comparison"}));await waitFor(()=>expect(lens.getLensInfo).toHaveBeenCalledTimes(2));
 expect(screen.queryByText("+9,007,199,254,740,993 bytes")).not.toBeInTheDocument();expect(lens.getStorageChange).toHaveBeenCalledTimes(1);
 finish(info("2026-10-17T14:31:00Z"));await waitFor(()=>expect(lens.getStorageChange).toHaveBeenLastCalledWith(expect.objectContaining({a:stamp("2026-10-17T13:31:00Z"),b:stamp("2026-10-17T14:31:00Z"),epochA:0n,epochB:0n,pageToken:""}),expect.anything()));
});
it("returns to the first page after changing a type filter and changing it back",async()=>{
 const r=response();r.totalCount=105;r.nextPageToken="next";vi.mocked(lens.getStorageChange).mockResolvedValue(r);
 renderApp(<Capacity/>,route);await region().findByRole("button",{name:"Sort by Change in range"});
 fireEvent.click(region().getByRole("button",{name:"Next contributors"}));await region().findByText("Showing 5–8 of 105 matching resource incarnations. Sorting applies before paging. Unknown changes remain last.");
 fireEvent.change(region().getByLabelText("Contributor type"),{target:{value:"kv"}});await region().findByRole("button",{name:"Sort by Change in range"});
 fireEvent.change(region().getByLabelText("Contributor type"),{target:{value:"all"}});
 expect(await region().findByText("Showing 1–4 of 105 matching resource incarnations. Sorting applies before paging. Unknown changes remain last.")).toBeInTheDocument();
});
it("distinguishes recreated resources by visible creation identity",async()=>{
 const r=response();r.rows[0].created=stamp("2026-10-17T12:00:00Z");const old=create(GetStorageChangeResponseSchema,{rows:[{...r.rows[0],id:"old-orders",created:stamp("2026-10-16T12:00:00Z")}]}).rows[0];r.rows.push(old);r.totalCount++;
 vi.mocked(lens.getStorageChange).mockResolvedValue(r);renderApp(<Capacity/>,route);await region().findByRole("button",{name:"Sort by Change in range"});
 expect(region().getByRole("button",{name:"Explore orders, file, created 2026-10-17T12:00:00Z"})).toBeInTheDocument();expect(region().getByRole("button",{name:"Explore orders, file, created 2026-10-16T12:00:00Z"})).toBeInTheDocument();expect(region().getByText("Created 2026-10-16T12:00:00Z")).toBeInTheDocument();
});
