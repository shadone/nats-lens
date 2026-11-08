import {GlobalQueryProvider} from "../lib/useGlobalQuery";
import {fireEvent,render,screen} from "@testing-library/react";
import {MemoryRouter} from "react-router";
import {expect,it,vi} from "vitest";
import {Trends} from "./Trends";
import {MiniLine} from "../charts/MiniLine";
import {InvestigationHandoff} from "./InvestigationContext";
import {buildInvestigationContext} from "../lib/investigationContext";
import type {InvestigationSelection} from "../lib/investigationContext";
vi.mock("../api/systems",()=>({useSystems:()=>({data:undefined})}));
vi.mock("../api/trendBatch",async load=>{
 const actual=await load<typeof import("../api/trendBatch")>();
 return {...actual,useTrends:(_spec:unknown,queries:{metric:string}[])=>({isPending:false,error:null,data:Object.fromEntries(queries.map(q=>[actual.trendKey(q),{series:{metric:q.metric,entity:{kind:"system",key:"",name:"shop",account:"",stream:"",consumer:"",server:""},unit:"count",stepMs:60000,points:[{time:1000,value:2},{time:2000,value:3}],topPoints:[]}}]))})};
});
function portable(selection:InvestigationSelection){return buildInvestigationContext({origin:"https://lens.example.com",pathname:"/",search:"",system:"shop",spec:{kind:"fixed",from:1000,to:3000},selection})}
it.each([
 ["Slow consumers",["cluster.slow_consumers"]],
 ["Assets",["cluster.assets_replicated","cluster.assets_single"]],
 ["Messages",["cluster.in_msgs_rate","cluster.out_msgs_rate"]],
 ["Bytes",["cluster.in_bytes_rate","cluster.out_bytes_rate"]],
])("carries every available API series and clear labels for %s",(label,metrics)=>{
 let selected:InvestigationSelection|undefined;
 render(<MemoryRouter><GlobalQueryProvider><InvestigationHandoff.Provider value={s=>{selected=s}}><Trends spec={{kind:"fixed",from:1000,to:3000}} start={1000} end={3000}/></InvestigationHandoff.Provider></GlobalQueryProvider></MemoryRouter>);
 fireEvent.click(screen.getByRole("slider",{name:`Inspect ${label}`}).parentElement!.querySelector("button")!);
 const c=portable(selected!);expect(c.chartLabel).toBe(label);expect(c.series?.map(s=>s.metric)).toEqual(metrics);expect(c.series?.every(s=>s.label.length>0)).toBe(true);
});
it("carries a known MiniLine series and explicitly describes unknown identities",()=>{
 let selected:InvestigationSelection|undefined;
 render(<InvestigationHandoff.Provider value={s=>{selected=s}}><MiniLine frame={{start:1000,end:3000}} points={[]} level="ok" label="Worker delivery" period="range" fmt={String} investigation={{metric:"consumer.delivered_rate",entity:{kind:"consumer",key:"APP/orders/worker",name:"worker",account:"APP",stream:"orders",consumer:"worker",server:""}}}/></InvestigationHandoff.Provider>);
 fireEvent.click(screen.getByRole("button",{name:"Copy as prompt"}));
 const known=portable(selected!);expect(known.series?.[0]).toMatchObject({metric:"consumer.delivered_rate",entity:{key:"APP/orders/worker"}});
 const unknown=portable({label:"Unidentified chart"});expect(unknown.chartLabel).toBe("Unidentified chart");expect(unknown.series?.[0].metric).toBeUndefined();expect(unknown.series?.[0].unavailableReason).toBeTruthy();
});
