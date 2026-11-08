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
