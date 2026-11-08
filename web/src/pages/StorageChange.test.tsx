import { create, fromJson } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { lens } from "../api/client";
import { GetStorageChangeResponseSchema } from "../gen/natslens/v1/storage_change_pb";
import { renderApp } from "../test/render";
import { StorageChange } from "./StorageChange";
vi.mock("../api/client",()=>({lens:{getStorageChange:vi.fn(),getLensInfo:vi.fn()}}));
const from="2026-10-17T13:00:00.000123Z",to="2026-10-17T14:00:00.000456Z";
const stamp=(s:string)=>fromJson(TimestampSchema,s);
const route=`/capacity/storage-change?system=example-prod&account=APP&from=${from}&to=${to}`;
function response(){return create(GetStorageChangeResponseSchema,{a:{requested:stamp(from),epoch:9007199254740993n,startedAt:stamp("2026-10-17T12:59:30Z"),finishedAt:stamp("2026-10-17T12:59:35Z"),inventoryComplete:true},b:{requested:stamp(to),epoch:9007199254740994n,startedAt:stamp("2026-10-17T13:59:30Z"),finishedAt:stamp("2026-10-17T13:59:35Z"),inventoryComplete:false,reasons:["missing_jsz"]},unit:"bytes",endpointPolicy:"completed_at_or_before_requested",rows:[{id:"bucket",rowKind:"resource",account:"APP",stream:"KV_order-cache",bucket:"order-cache",storage:"file",before:{logical:100n,observedReplicas:300n},after:{logical:50n,observedReplicas:150n},delta:{logical:-50n,observedReplicas:-150n},eventIds:[9007199254740999n],reasons:["observed_delete_event"]},{id:"missing",rowKind:"resource",account:"APP",stream:"orders",storage:"file",before:{logical:20n},after:{},delta:{},reasons:["not_observed_at_b"]}],totals:[{storage:"file",delta:{},knownContributions:{logical:-50n,observedReplicas:-150n},unknownRows:1}],totalCount:3,nextPageToken:"next"});}
beforeEach(()=>vi.resetAllMocks());
it("shows pinned windows, signed known contributions and unknown evidence distinctly",async()=>{
 vi.mocked(lens.getStorageChange).mockResolvedValue(response());renderApp(<StorageChange/>,route);
 expect(await screen.findByText(/9007199254740993/)).toBeInTheDocument();
 expect(screen.getByText(/2026-10-17T13:00:00.000123Z/)).toBeInTheDocument();
 expect(screen.getAllByText("−50 bytes").length).toBeGreaterThan(0);
 expect(screen.getAllByText(/Unknown/).length).toBeGreaterThan(0);
 expect(screen.getByText(/Missing JSZ evidence/)).toBeInTheDocument();
 const link=new URL(screen.getByRole("link",{name:"order-cache"}).getAttribute("href")!,"http://example.com");expect(link.pathname).toBe("/kv-stores/APP/order-cache");expect(link.searchParams.get("system")).toBe("example-prod");
 expect(screen.getByRole("link",{name:"Event 9007199254740999"})).toHaveAttribute("href",expect.stringContaining("id=9007199254740999"));
 await waitFor(()=>expect(screen.getByTestId("location").textContent).toContain("epochA=9007199254740993"));
 expect(lens.getStorageChange).toHaveBeenCalledWith(expect.objectContaining({a:stamp(from),b:stamp(to),account:"APP",system:"example-prod"}),expect.anything());
});
it("continues only the pinned comparison and exposes all-match counts",async()=>{
 const second=response();second.rows=create(GetStorageChangeResponseSchema,{rows:[{id:"last",rowKind:"resource",account:"APP",stream:"last",storage:"file",delta:{logical:1n}}]}).rows;second.nextPageToken="";
 vi.mocked(lens.getStorageChange).mockImplementation(async req=>req.pageToken?second:response());renderApp(<StorageChange/>,route);
 await screen.findByText("Showing 1–2 of 3 rows");await waitFor(()=>expect(screen.getByTestId("location").textContent).toContain("epochB=9007199254740994"));
 fireEvent.click(screen.getByRole("button",{name:"Next contributors"}));expect(await screen.findByRole("link",{name:"last"})).toBeInTheDocument();
 expect(lens.getStorageChange).toHaveBeenLastCalledWith(expect.objectContaining({pageToken:"next",epochA:9007199254740993n,epochB:9007199254740994n}),expect.anything());
});
it("rejects malformed pins without issuing a read",async()=>{renderApp(<StorageChange/>,route+"&epochA=bad");expect(await screen.findByRole("alert")).toHaveTextContent(/collection pin/);expect(lens.getStorageChange).not.toHaveBeenCalled();});
it("opens a disappeared resource around its last observed collection instead of the missing endpoint",async()=>{
 const r=response();r.nextPageToken="";r.rows[0].after=create(GetStorageChangeResponseSchema,{rows:[{after:{logical:0n,observedReplicas:0n}}]}).rows[0].after;r.rows[0].processesA=["S0"];r.rows[0].processesB=[];
 vi.mocked(lens.getStorageChange).mockResolvedValue(r);renderApp(<StorageChange/>,route);
 const link=new URL((await screen.findByRole("link",{name:"order-cache"})).getAttribute("href")!,"http://example.com");
 expect(link.searchParams.get("from")).toBe("2026-10-17T12:59:30Z");expect(link.searchParams.get("to")).toBe("2026-10-17T12:59:36Z");
});
it("keeps a zero-duration source window representable by the detail UI's millisecond range",async()=>{
 const {rangeProblem,parseRange,writeRange}=await import("../lib/timeRange");const r=response();r.a!.finishedAt=r.a!.startedAt;r.rows[0].processesA=["S0"];r.rows[0].processesB=[];
 vi.mocked(lens.getStorageChange).mockResolvedValue(r);renderApp(<StorageChange/>,route);
 const link=new URL((await screen.findByRole("link",{name:"order-cache"})).getAttribute("href")!,"http://example.com");expect(rangeProblem(link.searchParams,Date.parse(to))).toBeUndefined();expect(rangeProblem(writeRange(link.searchParams,parseRange(link.searchParams)),Date.parse(to))).toBeUndefined();
});

it("freezes a live preset against the loaded source clock before reading",async()=>{
 const {GetLensInfoResponseSchema}=await import("../gen/natslens/v1/info_pb");
 vi.mocked(lens.getLensInfo).mockResolvedValue(create(GetLensInfoResponseSchema,{now:stamp("2026-10-17T14:00:00Z"),nowFixed:true}));vi.mocked(lens.getStorageChange).mockResolvedValue(response());
 renderApp(<StorageChange/>,"/capacity/storage-change?system=example-prod&range=1h");
 await screen.findByRole("region",{name:"Storage contributors"});
 expect(lens.getStorageChange).toHaveBeenCalledWith(expect.objectContaining({a:stamp("2026-10-17T13:00:00Z"),b:stamp("2026-10-17T14:00:00Z"),epochA:0n,epochB:0n}),expect.anything());
});
it("names the resolved default system when pinning a shared URL",async()=>{
 const r=response();r.system="example-prod";vi.mocked(lens.getStorageChange).mockResolvedValue(r);
 renderApp(<StorageChange/>,route.replace("system=example-prod&",""));
 await waitFor(()=>expect(screen.getByTestId("location").textContent).toContain("system=example-prod"));
});

it("pins available partial endpoints before continuation without inventing a zero pin",async()=>{
 const r=response();r.system="example-prod";r.a!.epoch=0n;r.a!.reasons=["no_collection"];
 vi.mocked(lens.getStorageChange).mockResolvedValue(r);renderApp(<StorageChange/>,route.replace("system=example-prod&",""));
 await waitFor(()=>{const u=screen.getByTestId("location").textContent!;expect(u).toContain("system=example-prod");expect(u).toContain("epochB=9007199254740994");expect(u).not.toContain("epochA=")});
 fireEvent.click(screen.getByRole("button",{name:"Next contributors"}));
 await waitFor(()=>expect(lens.getStorageChange).toHaveBeenLastCalledWith(expect.objectContaining({system:"example-prod",epochA:0n,epochB:9007199254740994n,pageToken:"next"}),expect.anything()));
});
