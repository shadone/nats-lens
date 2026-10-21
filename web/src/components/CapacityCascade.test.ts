// The Capacity table's row-header rule must win over the
// shared `.tbl tbody th[scope="row"]` rule of events.css, or the stream
// names do not wrap (535ec7e) and sit on the baseline, not the middle.
// jsdom resolves the cascade by specificity, so it shows what a browser does.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const read = (f: string) => readFileSync(new URL(f, import.meta.url), "utf8");

describe("capacity.css row header against the shared table rules", () => {
  it("lets the stream name wrap and centres it", () => {
    const style = document.createElement("style");
    style.textContent = [read("../styles/pages.css"), read("../styles/events.css"), read("../styles/capacity.css")].join("\n");
    document.head.appendChild(style);
    document.body.innerHTML = `<table class="tbl cap-tbl"><tbody><tr><th scope="row"><a class="x cap-name">customer-notifications</a></th><td>3 days</td></tr></tbody></table>`;
    const th = document.querySelector("th")!;
    const cs = getComputedStyle(th);
    expect(cs.whiteSpace).toBe("normal");
    expect(cs.verticalAlign).toBe("middle");
    style.remove();
  });
});
