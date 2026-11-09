import {fireEvent,render,screen} from "@testing-library/react";
import {expect,it} from "vitest";
import {InfoTip} from "./InfoTip";

it.each(["hover","focus"])("dismisses repeated activation while %s remains",mode=>{
 render(<InfoTip label="Storage" text="Logical bytes in a bucket."/>);
 const button=screen.getByRole("button",{name:"About Storage"});
 if(mode==="hover")fireEvent.mouseEnter(button.parentElement!);else fireEvent.focus(button);
 expect(screen.getByRole("tooltip")).toBeInTheDocument();
 fireEvent.click(button);expect(button).toHaveAttribute("aria-expanded","true");
 fireEvent.click(button);expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
 expect(button).toHaveAttribute("aria-expanded","false");
});
