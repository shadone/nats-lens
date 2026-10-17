// The mute dialog against the real client, with a server that never
// answers the mute: the call times out and the dialog lets the user out.
import { act, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Finding } from "../api/model";
import { renderApp } from "../test/render";
import { MuteDialog } from "./MuteDialog";

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

/** GetLensInfo answers IAP mode; every other call hangs until aborted. */
function hangingServer() {
  return vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url.endsWith("/GetLensInfo")) {
      return Promise.resolve(new Response(JSON.stringify({ identityMode: "IDENTITY_MODE_IAP" }), { headers: { "content-type": "application/json" } }));
    }
    return new Promise<Response>((_, reject) => {
      init?.signal?.addEventListener("abort", () => reject(init.signal?.reason ?? new DOMException("aborted", "AbortError")));
    });
  });
}

describe("MuteDialog when the server never answers", () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("shows the timeout after 45 seconds and lets the user cancel", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.stubGlobal("fetch", hangingServer());
    renderApp(<MuteDialog finding={finding} onClose={() => {}} />);
    await screen.findByText(/IAP identity/);
    await userEvent.type(screen.getByLabelText("Reason"), "noise");
    await userEvent.click(screen.getByRole("button", { name: "Mute" }));
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(44_000);
    });
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000);
    });
    expect(await screen.findByText(/deadline_exceeded/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeEnabled();
  });
});
