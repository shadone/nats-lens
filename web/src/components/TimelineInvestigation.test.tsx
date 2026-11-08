import {fireEvent,render,screen} from "@testing-library/react";
import {MemoryRouter} from "react-router";
import {expect,it,vi} from "vitest";
import {useFindings} from "../api/queries";
import {TimelineFindingRange} from "./TimelineInvestigation";
vi.mock("../api/queries",()=>({useFindings:vi.fn()}));
vi.mock("../lib/useGlobalQuery",()=>({useGlobalQuery:()=>"?range=1h&system=demo&account=APP"}));
const from=Date.parse("2026-10-17T23:59:00Z"),end=from+60000;
const finding={id:"consumer-006:APP/orders/worker",checkCode:"consumer-006",checkName:"Delivery stopped",severity:"warning",state:"resolved",detail:"No deliveries observed",remediation:"Check the receiving application",transitionsLastHour:1};
it("reads all states in a fixed interval and pages explicitly without changing scope",()=>{
 vi.mocked(useFindings).mockImplementation((_spec,f)=>({data:{items:[finding],totalCount:51,nextPageToken:f.pageToken?"":"next"},isPending:false,error:null}) as ReturnType<typeof useFindings>);
 render(<MemoryRouter initialEntries={["/?system=demo"]}><TimelineFindingRange from={from} end={end} checkCode="consumer-006"/></MemoryRouter>);
 expect(useFindings).toHaveBeenLastCalledWith({kind:"fixed",from,to:end},expect.objectContaining({checkCode:"consumer-006",includeMuted:true,pageSize:50,states:["firing","changed","flapping","resolved"]}));
 expect(screen.getByText("Resolved")).toBeInTheDocument();
 const link=new URL(screen.getByRole("link",{name:"Open finding details"}).getAttribute("href")!,"http://example.com");
 expect(link.searchParams.get("system")).toBe("demo");expect(link.searchParams.has("range")).toBe(false);
 expect(Date.parse(link.searchParams.get("from")!)).toBe(from);expect(Date.parse(link.searchParams.get("to")!)).toBe(end);
 fireEvent.click(screen.getByRole("button",{name:"Next"}));
 expect(useFindings).toHaveBeenLastCalledWith({kind:"fixed",from,to:end},expect.objectContaining({pageToken:"next",pageSize:50}));
 fireEvent.click(screen.getByText("Remediation"));expect(screen.getByText("Check the receiving application")).toBeInTheDocument();
});
it("does not describe missing retained evidence as a healthy interval",()=>{
 vi.mocked(useFindings).mockReturnValue({data:{items:[],totalCount:0,nextPageToken:""},isPending:false,error:null} as unknown as ReturnType<typeof useFindings>);
 render(<MemoryRouter><TimelineFindingRange from={from} end={end}/></MemoryRouter>);
 expect(screen.getByText(/Evidence may have expired or not been captured/)).toBeInTheDocument();
});
it("uses the selected resource account for a broader findings link",()=>{
 vi.mocked(useFindings).mockReturnValue({data:{items:[],totalCount:0,nextPageToken:""},isPending:false,error:null} as unknown as ReturnType<typeof useFindings>);
 render(<MemoryRouter><TimelineFindingRange from={from} end={end} entity={{kind:"consumer",key:"OTHER/orders/worker",name:"worker",account:"OTHER",stream:"orders",consumer:"worker",server:""}}/></MemoryRouter>);
 const u=new URL(screen.getByRole('link',{name:'Open system-wide findings page'}).getAttribute('href')!,'http://example.com');expect(u.searchParams.get('account')).toBe('OTHER');
});
