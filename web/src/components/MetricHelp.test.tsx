import {create} from "@bufbuild/protobuf";
import {fireEvent,screen} from "@testing-library/react";
import {expect,it,vi} from "vitest";
import {lens} from "../api/client";
import {ListMetricsResponseSchema} from "../gen/natslens/v1/timeseries_pb";
import {renderApp} from "../test/render";
import {TrendChart} from "./TrendChart";

vi.mock("../api/client",()=>({lens:{listMetrics:vi.fn()}}));
it("loads authoritative chart help only when requested and shares the catalogue",async()=>{
 vi.mocked(lens.listMetrics).mockResolvedValue(create(ListMetricsResponseSchema,{metrics:[{id:"cluster.cpu_percent",description:"CPU use across servers, percent of one core; averaged per bucket."}]}));
 renderApp(<><TrendChart title="CPU" investigation={{metric:"cluster.cpu_percent"}} start={0} end={1000} layers={[]}/><TrendChart title="CPU elsewhere" investigation={{metric:"cluster.cpu_percent"}} start={0} end={1000} layers={[]}/></>);
 expect(lens.listMetrics).not.toHaveBeenCalled();
 const button=screen.getByRole("button",{name:"About CPU"});
 fireEvent.focus(button);
 expect(await screen.findByText(/percent of one core/)).toBeInTheDocument();
 expect(screen.getByRole("tooltip").parentElement).toBe(document.body);
 fireEvent.keyDown(button,{key:"Escape"});
 expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
 fireEvent.click(screen.getByRole("button",{name:"About CPU elsewhere"}));
 expect(await screen.findByText(/percent of one core/)).toBeInTheDocument();
 expect(lens.listMetrics).toHaveBeenCalledTimes(1);
});

 it("keeps catalogue failure separate from chart samples",async()=>{
  vi.mocked(lens.listMetrics).mockRejectedValue(new Error("unavailable"));
  renderApp(<TrendChart title="Memory" investigation={{metric:"cluster.mem_bytes"}} start={0} end={1000} layers={[]}/>);
  fireEvent.click(screen.getByRole("button",{name:"About Memory"}));
  expect(await screen.findByText(/Metric description unavailable/)).toBeInTheDocument();
 });
 it("discloses unknown identities instead of inventing a description",()=>{
  renderApp(<TrendChart title="Unknown" start={0} end={1000} layers={[]}/>);
  fireEvent.click(screen.getByRole("button",{name:"About Unknown"}));
  expect(screen.getByRole("tooltip")).toHaveTextContent("Metric identity is unavailable");
 });

it("uses the same help control on resource detail charts",async()=>{
 vi.mocked(lens.listMetrics).mockResolvedValue(create(ListMetricsResponseSchema,{metrics:[{id:"stream.bytes",description:"Bytes stored, from the leader's view; averaged per bucket."}]}));
 const {SeriesChart}=await import("../charts/SeriesChart");
 renderApp(<SeriesChart title="Stored bytes" series={{metric:"stream.bytes",unit:"bytes",stepMs:1000,points:[],min:0,max:0}} start={0} end={1000}/>);
 fireEvent.focus(screen.getByRole("button",{name:"About Stored bytes"}));
 expect(await screen.findByText(/from the leader's view/)).toBeInTheDocument();
});
