import {StrictMode} from "react";
import {render,screen} from "@testing-library/react";
import {afterEach,expect,it,vi} from "vitest";
import {Dialog} from "./Dialog";

afterEach(()=>{vi.restoreAllMocks();vi.unstubAllGlobals();document.body.removeAttribute("style");document.documentElement.removeAttribute("style")});
it("locks the background at its position and restores previous styles when closed",()=>{
 vi.stubGlobal("scrollY",420);vi.stubGlobal("scrollX",7);const scroll=vi.spyOn(window,"scrollTo").mockImplementation(()=>{});
 document.body.style.position="relative";document.documentElement.style.overflow="auto";
 const {rerender}=render(<Dialog open title="Timeline" onClose={()=>{}}>Evidence</Dialog>);
 expect(document.body.style.position).toBe("fixed");
 expect(document.body.style.top).toBe("-420px");
 expect(document.documentElement.style.overflow).toBe("hidden");
 rerender(<Dialog open={false} title="Timeline" onClose={()=>{}}>Evidence</Dialog>);
 expect(document.body.style.position).toBe("relative");expect(document.body.style.top).toBe("");
 expect(document.documentElement.style.overflow).toBe("auto");expect(scroll).toHaveBeenLastCalledWith(7,420);
});
it("keeps the lock until the final modal closes, including StrictMode and unmount",()=>{
 vi.spyOn(window,"scrollTo").mockImplementation(()=>{});
 function App({first,second}:{first:boolean;second:boolean}){return <StrictMode><Dialog open={first} title="First" onClose={()=>{}}>First content</Dialog><Dialog open={second} title="Second" onClose={()=>{}}>Second content</Dialog></StrictMode>}
 const {rerender,unmount}=render(<App first second/>);
 expect(screen.getAllByRole("dialog")).toHaveLength(2);
 rerender(<App first={false} second/>);expect(document.body.style.position).toBe("fixed");
 unmount();expect(document.body.style.position).toBe("");expect(document.documentElement.style.overflow).toBe("");
});
