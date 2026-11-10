import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { Entity, Finding, Mute } from "../api/model";
import { groupCauses } from "../lib/causeGroups";
import { renderApp } from "../test/render";
import { CauseGroups } from "./CauseGroups";

const consumer = (name: string): Entity => ({ kind: "consumer", key: `APP/order-jobs/${name}`, name, account: "APP", stream: "order-jobs", consumer: name, server: "" });

function f(name: string, extra: Partial<Finding> = {}): Finding {
  return {
    id: `consumer-016:APP/order-jobs/${name}`,
    checkCode: "consumer-016",
    checkName: "Consumer with one replica",
    severity: "info",
    state: "firing",
    entity: consumer(name),
    detail: "",
    remediation: "",
    relatedConfig: [],
    relatedConfigSource: "",
    transitionsLastHour: 0,
    acceptable: true,
    ...extra,
  };
}

const mute = (accepted: boolean, reason: string): Mute => ({ id: reason, checkCode: "consumer-016", reason, mutedBy: "op", accepted, unmutedBy: "", endReason: "" });

const card = (onAccept: (f: Finding, preselect?: Finding[]) => void) => ({ read: { loading: false, failed: false }, system: "", scoped: false, onAccept });

describe("CauseGroups and acceptance", () => {
  it("offers to accept every entity of a card of one acceptable check under one stream", async () => {
    const onAccept = vi.fn();
    const { groups, muted } = groupCauses([f("a"), f("b"), f("c")]);
    renderApp(<CauseGroups groups={groups} muted={muted} onMute={() => {}} card={card(onAccept)} />);
    await userEvent.click(screen.getByRole("button", { name: "Accept consumer-016 on all 3 consumers of stream order-jobs as by design" }));
    // Exactly the card's other findings, not whatever a later read returns.
    const [first, pre] = onAccept.mock.calls[0];
    expect(first.id).toBe("consumer-016:APP/order-jobs/a");
    expect(pre.map((x: Finding) => x.id)).toEqual(["consumer-016:APP/order-jobs/b", "consumer-016:APP/order-jobs/c"]);
  });

  it("says it accepts the first 100 on a card of more, and passes the first 99 others", async () => {
    const onAccept = vi.fn();
    const names = Array.from({ length: 150 }, (_, i) => `w${String(i).padStart(3, "0")}`);
    const { groups, muted } = groupCauses(names.map((n) => f(n)));
    renderApp(<CauseGroups groups={groups} muted={muted} onMute={() => {}} card={card(onAccept)} />);
    const btn = screen.getByRole("button", { name: "Accept consumer-016 on the first 100 of 150 consumers of stream order-jobs as by design" });
    expect(btn).toHaveTextContent("Accept the first 100 as by design…");
    await userEvent.click(btn);
    const [first, pre] = onAccept.mock.calls[0];
    expect(pre).toHaveLength(99);
    expect(pre.map((x: Finding) => x.id)).not.toContain(first.id);
  });

  it("offers no Accept all when a finding of the card is not acceptable", () => {
    const { groups, muted } = groupCauses([f("a"), f("b", { acceptable: false })]);
    renderApp(<CauseGroups groups={groups} muted={muted} onMute={() => {}} card={card(vi.fn())} />);
    expect(screen.queryByRole("button", { name: /^Accept consumer-016 on all/ })).toBeNull();
  });

  it("lists accepted findings in a strip of their own, apart from the muted ones", () => {
    const { groups, muted } = groupCauses([f("a", { mute: mute(false, "upgrade") }), f("b", { mute: mute(true, "batch") }), f("c", { mute: mute(true, "batch") })]);
    renderApp(<CauseGroups groups={groups} muted={muted} onMute={() => {}} />);
    expect(screen.getByRole("region", { name: "Muted findings" })).toHaveTextContent("1 muted finding");
    const acc = screen.getByRole("region", { name: "Accepted findings" });
    expect(acc).toHaveTextContent("2 accepted findings");
    expect(screen.getByRole("link", { name: "Review accepted findings" })).toHaveAttribute("href", expect.stringContaining("/findings/accepted"));
  });
});
