// The mute dialog stays busy until the pages' data has refetched, so the
// button that opened it cannot mute the same finding twice meanwhile.
import { useQuery } from "@tanstack/react-query";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { Finding } from "../api/model";
import * as requests from "../api/requests";
import { renderApp } from "../test/render";
import { MuteDialog } from "./MuteDialog";

vi.mock("../api/requests", () => ({ muteFinding: vi.fn(), getLensInfo: vi.fn() }));

const finding: Finding = {
  id: "stream-006:APP/audit-archive",
  checkCode: "stream-006",
  checkName: "Stream reaches its byte limit soon",
  severity: "warning",
  state: "firing",
  entity: { kind: "stream", key: "APP/audit-archive", name: "audit-archive", account: "APP", stream: "audit-archive", consumer: "", server: "" },
  detail: "",
  remediation: "",
  relatedConfig: [],
  relatedConfigSource: "",
  transitionsLastHour: 0,
};

describe("MuteDialog and the refetch", () => {
  it("stays on Muting… until the page's data has refetched", async () => {
    vi.mocked(requests.getLensInfo).mockResolvedValue({ identityMode: "iap", version: "dev", cluster: "c", collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: true });
    vi.mocked(requests.muteFinding).mockResolvedValue(undefined);
    let land = () => {};
    let calls = 0;
    const fetchPage = vi.fn(() => {
      calls++;
      return calls === 1 ? Promise.resolve(1) : new Promise<number>((r) => (land = () => r(2)));
    });
    function Page() {
      useQuery({ queryKey: ["page"], queryFn: fetchPage });
      return null;
    }
    const onClose = vi.fn();
    renderApp(
      <>
        <Page />
        <MuteDialog finding={finding} onClose={onClose} />
      </>,
    );
    await waitFor(() => expect(fetchPage).toHaveBeenCalledTimes(1));
    await userEvent.type(await screen.findByLabelText("Reason"), "noise");
    await userEvent.click(screen.getByRole("button", { name: "Mute" }));
    await waitFor(() => expect(fetchPage).toHaveBeenCalledTimes(2));
    expect(screen.getByRole("button", { name: "Muting…" })).toBeDisabled();
    expect(onClose).not.toHaveBeenCalled();
    land();
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });
});
