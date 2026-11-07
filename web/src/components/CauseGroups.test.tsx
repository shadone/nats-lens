import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Entity, Finding } from "../api/model";
import { groupCauses } from "../lib/causeGroups";
import { renderApp } from "../test/render";
import { CauseGroups } from "./CauseGroups";

function consumerEnt(stream: string, name: string): Entity {
  return { kind: "consumer", key: `APP/${stream}/${name}`, name, account: "APP", stream, consumer: name, server: "" };
}

function f(id: string, e: Entity, extra: Partial<Finding> = {}): Finding {
  return {
    id,
    checkCode: "consumer-009",
    checkName: "Consumer creation time differs across servers",
    severity: "warning",
    state: "firing",
    entity: e,
    detail: `Detail of ${e.name}.`,
    remediation: "",
    relatedConfig: [],
    relatedConfigSource: "",
    transitionsLastHour: 0,
    ...extra,
  };
}

function show(fs: Finding[]) {
  const { groups, muted } = groupCauses(fs);
  renderApp(<CauseGroups groups={groups} muted={muted} onMute={() => {}} />);
}

describe("CauseGroups", () => {
  it("labels the accounts of two cards apart when they share a name", () => {
    const a = "ADSUPPLIERSFAKEACCOUNTKEYEXAMPLEGLOBAL7Q2M4XKV3WPJ6RT5NH";
    const b = "AXSUPPLIERSOTHERACCOUNTKEYEXAMPLEGLOBAL7Q2M4XKV3WPJ6RT5N";
    const streamEnt = (acc: string, stream: string): Entity => ({ kind: "stream", key: `${acc}/${stream}`, name: stream, account: acc, accountName: "SUPPLIERS", stream, consumer: "", server: "" });
    show(["s1", "s2"].flatMap((st) => [a, b].map((acc) => f(`${acc}${st}`, streamEnt(acc, st), { checkCode: "stream-005", checkName: "Stream without any limit" }))));
    const where = screen.getAllByText(/2 streams in account/).map((e) => e.textContent);
    expect(where.sort()).toEqual(["2 streams in account SUPPLIERS (ADSU…)", "2 streams in account SUPPLIERS (AXSU…)"]);
  });

  it("does not name one stream for consumers of a cause on two streams", () => {
    const cause = { id: "ev-2", kind: "server.restart", summary: "nats-1 restarted" };
    show([f("a", consumerEnt("order-jobs", "a"), { cause }), f("b", consumerEnt("media-windows", "b"), { cause })]);
    const card = screen.getByRole("article");
    expect(within(card).getByText(/2 consumers on 2 streams/)).toBeInTheDocument();
    expect(within(card).queryByText(/on stream/)).toBeNull();
  });

  it("names the stream when every consumer is on it", () => {
    show([f("a", consumerEnt("order-jobs", "a")), f("b", consumerEnt("order-jobs", "b"))]);
    expect(screen.getByText(/2 consumers on stream/)).toHaveTextContent("2 consumers on stream order-jobs");
  });

  it("names a single finding's buttons by its check and entity", () => {
    show([f("a", consumerEnt("order-jobs", "resize-worker"))]);
    expect(screen.getByRole("link", { name: "Open consumer-009 on resize-worker" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Mute consumer-009 on resize-worker" })).toBeInTheDocument();
  });
});
