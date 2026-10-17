import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { LensEvent } from "../api/model";
import { eventPath } from "../lib/view";
import { renderApp } from "../test/render";
import { EventRow } from "./EventRow";

const awkward = "nats:A/s?x=1&range=7d #2";

const ev: LensEvent = {
  id: awkward,
  time: Date.now() - 60_000,
  source: "nats",
  kind: "consumer.deleted",
  type: "CONSUMER.DELETED",
  summary: "c on s",
  server: "",
  account: "A",
  stream: "s",
  consumer: "c",
  clientName: "",
  clientHost: "",
  reason: "",
  raised: [],
  raw: "",
};

describe("event links", () => {
  it("encode the event id so it reads back whole", () => {
    expect(eventPath(awkward)).toBe("/events?id=nats%3AA%2Fs%3Fx%3D1%26range%3D7d%20%232");
    const url = new URL(eventPath(awkward), "http://x");
    expect(url.searchParams.get("id")).toBe(awkward);
    expect(url.searchParams.has("range")).toBe(false);
  });

  it("keep the whole id in the row's link", () => {
    renderApp(<EventRow e={ev} end={Date.now()} open onToggle={() => {}} />, "/events?range=6h");
    const href = screen.getByRole("link", { name: "Link to this event" }).getAttribute("href") ?? "";
    const url = new URL(href, "http://x");
    expect(url.pathname).toBe("/events");
    expect(url.searchParams.get("id")).toBe(awkward);
    expect(url.searchParams.get("range")).toBe("6h");
  });
});
