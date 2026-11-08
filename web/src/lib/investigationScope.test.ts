import {expect,it} from "vitest";
import {buildInvestigationContext} from "./investigationContext";
const build=(pathname:string,search:string)=>buildInvestigationContext({origin:"https://lens.example.com",pathname,search,system:"shop",spec:{kind:"fixed",from:1000,to:3000}});
it.each([
 ["/consumers","?account=OTHER&stream=APP/orders","APP/orders"],
 ["/events","?account=OTHER&server=old&stream=APP/orders","APP/orders"],
 ["/events","?account=OTHER&server=old&stream=OLD/stale&consumer=APP/orders/worker","APP/orders/worker"],
 ["/streams/APP/orders","?account=OTHER&stream=OLD/stale&consumer=OLD/stale/old","APP/orders"],
 ["/consumers/APP/orders/worker","?account=OTHER&consumer=OLD/stale/old","APP/orders/worker"],
])("copies the selected compound identity ahead of retained filters on %s",(path,search,key)=>{
 const c=build(path,search);expect(c.entity?.key).toBe(key);expect(c.account).toBe("APP");expect(c.filters.account).toBe("OTHER");
});
it("keeps a selected chart resource ahead of unrelated page selectors",()=>{
 const c=buildInvestigationContext({origin:"https://lens.example.com",pathname:"/events",search:"?consumer=OLD/stale/old",system:"shop",spec:{kind:"fixed",from:1000,to:3000},selection:{metric:"stream.bytes",entity:{kind:"stream",key:"APP/orders",account:"APP",stream:"orders",consumer:"",server:"",name:"orders"}}});
 expect(c.entity?.key).toBe("APP/orders");expect(c.account).toBe("APP");
});
