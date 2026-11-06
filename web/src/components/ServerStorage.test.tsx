import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { ServerRow } from "../api/servers";
import { renderApp } from "../test/render";
import { ServerList } from "./ServerList";

const row = (name: string, overrides: Partial<ServerRow> = {}): ServerRow => ({
  name, serverId: name, version: "2.15.0", cluster: "shop", role: "follower", present: true,
  status: "ok", healthz: "ok", restarts: [], tags: [], ...overrides,
});

describe("server list storage", () => {
  it("shows stored file bytes and configured limits, including zero and unknown limits", async () => {
    renderApp(<ServerList servers={[
      row("bounded", { jsStorageBytes: 1 << 20, jsMaxStorageBytes: 2 << 20 }),
      row("empty", { jsStorageBytes: 0 }),
      row("unknown", { jsMaxStorageBytes: 2 << 20 }),
      row("zero-limit", { jsStorageBytes: 1 << 20, jsMaxStorageBytes: 0 }),
      row("disabled", { role: "no JetStream", jsStorageBytes: 0 }),
      row("down", { present: false, jsStorageBytes: 1 << 20, jsMaxStorageBytes: 2 << 20 }),
    ]} />);
    await userEvent.click(screen.getByRole("button", { name: "All 6" }));
    const storage = (name: string) => within(screen.getByRole("link", { name }).closest("tr")!).getAllByRole("cell")[9];
    expect(screen.getByRole("columnheader", { name: "JetStream file storage" })).toBeInTheDocument();
    expect(storage("bounded")).toHaveTextContent("1 MiB / 2 MiB");
    expect(within(storage("bounded")).getByRole("img", { name: "50% of configured JetStream limit" })).toBeInTheDocument();
    expect(storage("empty")).toHaveTextContent("0 B · limit unknown");
    expect(storage("unknown")).toHaveTextContent("not reported · limit 2 MiB");
    expect(storage("zero-limit")).toHaveTextContent("1 MiB · limit unknown");
    expect(storage("disabled")).toHaveTextContent("no JetStream");
    expect(storage("down")).toHaveTextContent("did not answer");
    expect(screen.getByText(/Filesystem used and capacity are not collected/)).toBeInTheDocument();
  });
});
