import { Code, ConnectError } from "@connectrpc/connect";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { useNavigate } from "react-router";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Finding } from "../api/model";
import * as requests from "../api/requests";
import { validateMute } from "../lib/muteForm";
import { renderApp } from "../test/render";
import { MuteDialog, UnmuteDialog } from "./MuteDialog";

vi.mock("../api/requests", () => ({ muteFinding: vi.fn(), unmuteFinding: vi.fn(), getLensInfo: vi.fn() }));
const muteFinding = vi.mocked(requests.muteFinding);
const getLensInfo = vi.mocked(requests.getLensInfo);
const info = (identityMode: "iap" | "request") => ({ identityMode, version: "dev", cluster: "c", collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: true });

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

const NOW = Date.parse("2026-10-17T14:30:00Z");
const form = { reason: "Retention cut in review", expiry: "24h", customExpiry: "", name: "alice" };

describe("validateMute", () => {
  it("accepts a reason, a name and a preset expiry", () => {
    expect(validateMute(form, NOW, true)).toEqual({});
  });

  it("requires a reason and, when asked, a name", () => {
    const e = validateMute({ ...form, reason: "  ", name: "" }, NOW, true);
    expect(Object.keys(e).sort()).toEqual(["name", "reason"]);
    expect(validateMute({ ...form, name: "" }, NOW, false)).toEqual({});
  });

  it("limits the reason to 1000 characters", () => {
    expect(validateMute({ ...form, reason: "x".repeat(1000) }, NOW, true).reason).toBeUndefined();
    expect(validateMute({ ...form, reason: "x".repeat(1001) }, NOW, true).reason).toMatch(/1000/);
  });

  it("keeps the expiry between a minute and 90 days ahead, read as UTC", () => {
    const at = (v: string) => validateMute({ ...form, expiry: "custom", customExpiry: v }, NOW, true).expiry;
    expect(at("")).toMatch(/Choose/);
    expect(at("2026-10-17T14:30")).toMatch(/minute/);
    expect(at("2026-10-17T14:32")).toBeUndefined();
    expect(at("2027-01-15T14:00")).toBeUndefined();
    expect(at("2027-01-15T15:00")).toMatch(/90 days/);
  });
});

describe("MuteDialog", () => {
  beforeEach(() => {
    localStorage.clear();
    muteFinding.mockReset();
    getLensInfo.mockResolvedValue(info("request"));
  });

  it("shows the errors and sends nothing while the form is invalid", async () => {
    renderApp(<MuteDialog finding={finding} onClose={() => {}} />);
    await userEvent.click(screen.getByRole("button", { name: "Mute" }));
    expect(screen.getByText(/Give a reason/)).toBeInTheDocument();
    expect(screen.getByText(/Enter your name/)).toBeInTheDocument();
    expect(screen.getByLabelText("Reason")).toHaveAttribute("aria-invalid", "true");
    expect(muteFinding).not.toHaveBeenCalled();
  });

  it("mutes the check for the entity until the chosen expiry, then closes", async () => {
    muteFinding.mockResolvedValue({ id: "1", checkCode: "stream-006", reason: "r", mutedBy: "alice", unmutedBy: "", accepted: false, endReason: "" });
    const onClose = vi.fn();
    renderApp(<MuteDialog finding={finding} onClose={onClose} />);
    await userEvent.type(screen.getByLabelText("Reason"), "Retention cut in review");
    await userEvent.type(screen.getByLabelText("Your name"), "alice");
    fireEvent.change(screen.getByLabelText("Ends"), { target: { value: "7d" } });
    const before = Date.now();
    await userEvent.click(screen.getByRole("button", { name: "Mute" }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    const req = muteFinding.mock.calls[0][1];
    expect(req).toMatchObject({ checkCode: "stream-006", reason: "Retention cut in review", mutedBy: "alice" });
    expect(req.entity?.key).toBe("APP/audit-archive");
    expect(req.expiresAt - before).toBeGreaterThanOrEqual(7 * 86_400_000);
    expect(req.expiresAt - before).toBeLessThan(7 * 86_400_000 + 60_000);
    expect(localStorage.getItem("nats-lens-name")).toBe("alice");
  });

  it("mutes every entity of the check when asked", async () => {
    muteFinding.mockResolvedValue(undefined);
    renderApp(<MuteDialog finding={finding} onClose={() => {}} />);
    await userEvent.click(screen.getByLabelText(/Every entity of stream-006/));
    await userEvent.type(screen.getByLabelText("Reason"), "noise");
    await userEvent.type(screen.getByLabelText("Your name"), "alice");
    await userEvent.click(screen.getByRole("button", { name: "Mute" }));
    await waitFor(() => expect(muteFinding).toHaveBeenCalled());
    expect(muteFinding.mock.calls[0][1].entity).toBeUndefined();
  });

  it("asks for no name when the server takes identity from IAP", async () => {
    getLensInfo.mockResolvedValue(info("iap"));
    localStorage.setItem("nats-lens-iap", "1");
    muteFinding.mockResolvedValue(undefined);
    renderApp(<MuteDialog finding={finding} onClose={() => {}} />);
    expect(await screen.findByText(/recorded as your IAP identity/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Your name")).toBeNull();
    await userEvent.type(screen.getByLabelText("Reason"), "noise");
    await userEvent.click(screen.getByRole("button", { name: "Mute" }));
    await waitFor(() => expect(muteFinding).toHaveBeenCalled());
    expect(muteFinding.mock.calls[0][1].mutedBy).toBe("");
  });

  it("asks for a name when the request names the user, whatever an old flag says", async () => {
    getLensInfo.mockResolvedValue(info("request"));
    localStorage.setItem("nats-lens-iap", "1");
    renderApp(<MuteDialog finding={finding} onClose={() => {}} />);
    expect(await screen.findByLabelText("Your name")).toBeInTheDocument();
  });

  it("shows the server's refusal", async () => {
    muteFinding.mockRejectedValue(new Error("expires_at must be at most 90 days ahead"));
    renderApp(<MuteDialog finding={finding} onClose={() => {}} />);
    await userEvent.type(screen.getByLabelText("Reason"), "noise");
    await userEvent.type(screen.getByLabelText("Your name"), "alice");
    await userEvent.click(screen.getByRole("button", { name: "Mute" }));
    expect(await screen.findByText(/at most 90 days/)).toBeInTheDocument();
  });

  it("names the account in a refusal of a mute of an accepted entity", async () => {
    getLensInfo.mockResolvedValue(info("iap"));
    const id = "AFAKEAPPACCOUNT";
    const f: Finding = { ...finding, id: `stream-006:${id}/audit-archive`, entity: { ...finding.entity!, key: `${id}/audit-archive`, account: id, accountName: "APP" } };
    muteFinding.mockRejectedValue(
      new ConnectError(`already exists: stream-006 for stream ${id}/audit-archive: accepted as by design until 2027-01-21 14:30 UTC; end the acceptance before muting`, Code.AlreadyExists),
    );
    renderApp(<MuteDialog finding={f} onClose={() => {}} />);
    await userEvent.type(screen.getByLabelText("Reason"), "noise");
    await userEvent.click(screen.getByRole("button", { name: "Mute" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /^already_exists: stream-006 for stream APP\/audit-archive: accepted as by design until 2027-01-21 14:30 UTC; end the acceptance before muting$/,
    );
  });
});

describe("MuteDialog between openings", () => {
  beforeEach(() => {
    localStorage.clear();
    muteFinding.mockReset();
    getLensInfo.mockResolvedValue(info("request"));
  });

  function Host() {
    const [open, setOpen] = useState<Finding>();
    return (
      <>
        <button type="button" onClick={() => setOpen(finding)}>
          open
        </button>
        <MuteDialog finding={open} onClose={() => setOpen(undefined)} />
      </>
    );
  }

  it("starts with an empty form and no error after a cancelled attempt", async () => {
    muteFinding.mockRejectedValue(new Error("boom"));
    renderApp(<Host />);
    await userEvent.click(screen.getByText("open"));
    await userEvent.type(screen.getByLabelText("Reason"), "first reason");
    await userEvent.type(screen.getByLabelText("Your name"), "me");
    await userEvent.click(screen.getByLabelText(/Every entity of stream-006/));
    await userEvent.click(screen.getByRole("button", { name: "Mute" }));
    await screen.findByText("boom");
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await userEvent.click(screen.getByText("open"));
    expect(screen.getByLabelText("Reason")).toHaveValue("");
    expect(screen.getByLabelText(/This entity/)).toBeChecked();
    expect(screen.queryByText("boom")).toBeNull();
  });
});

describe("dialogs while a request is pending", () => {
  const unmuteFinding = vi.mocked(requests.unmuteFinding);
  beforeEach(() => {
    localStorage.clear();
    muteFinding.mockReset();
    unmuteFinding.mockReset();
    getLensInfo.mockResolvedValue(info("iap"));
  });

  it("cannot be closed while a mute is pending, and keeps a later error in view", async () => {
    let reject!: (e: Error) => void;
    muteFinding.mockReturnValue(new Promise((_, r) => (reject = r)));
    const onClose = vi.fn();
    renderApp(<MuteDialog finding={finding} onClose={onClose} />);
    await screen.findByText(/IAP identity/);
    await userEvent.type(screen.getByLabelText("Reason"), "noise");
    await userEvent.click(screen.getByRole("button", { name: "Mute" }));
    expect(screen.getByRole("button", { name: "Muting…" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Close" })).toBeDisabled();
    fireEvent(document.querySelector("dialog")!, new Event("cancel", { cancelable: true }));
    expect(onClose).not.toHaveBeenCalled();
    await act(async () => reject(new Error("boom")));
    expect(await screen.findByText("boom")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeEnabled();
    expect(muteFinding).toHaveBeenCalledTimes(1);
  });

  it("says Lifting… and cannot be closed while an unmute is pending", async () => {
    unmuteFinding.mockReturnValue(new Promise(() => {}));
    const onClose = vi.fn();
    renderApp(<UnmuteDialog mute={{ id: "1", checkCode: "stream-006", reason: "r", mutedBy: "alice", unmutedBy: "", accepted: false, endReason: "" }} onClose={onClose} />);
    await screen.findByText(/IAP identity/);
    await userEvent.click(screen.getByRole("button", { name: "Lift mute" }));
    expect(screen.getByRole("button", { name: "Lifting…" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    fireEvent(document.querySelector("dialog")!, new Event("cancel", { cancelable: true }));
    expect(onClose).not.toHaveBeenCalled();
  });
});

describe("the system a mute goes to", () => {
  let go: (to: string) => void = () => {};
  function Nav() {
    go = useNavigate();
    return null;
  }

  beforeEach(() => {
    localStorage.clear();
    muteFinding.mockReset();
    vi.mocked(requests.unmuteFinding).mockReset();
    getLensInfo.mockResolvedValue(info("iap"));
  });

  it("is the system of the page when the dialog opened, also after the URL changed", async () => {
    muteFinding.mockResolvedValue(undefined);
    renderApp(
      <>
        <Nav />
        <MuteDialog finding={finding} onClose={() => {}} />
      </>,
      "/findings?system=sys-a",
    );
    await userEvent.type(screen.getByLabelText("Reason"), "noise");
    act(() => go("/findings?system=sys-b"));
    await userEvent.click(screen.getByRole("button", { name: "Mute" }));
    await waitFor(() => expect(muteFinding).toHaveBeenCalled());
    expect(muteFinding.mock.calls[0][0]).toBe("sys-a");
  });

  it("lifts a mute on the system of the page when the dialog opened", async () => {
    vi.mocked(requests.unmuteFinding).mockResolvedValue(undefined);
    const m = { id: "m1", checkCode: "stream-006", reason: "r", mutedBy: "alice", unmutedBy: "", accepted: false, endReason: "" } as never;
    renderApp(
      <>
        <Nav />
        <UnmuteDialog mute={m} onClose={() => {}} />
      </>,
      "/findings?system=sys-a",
    );
    act(() => go("/findings?system=sys-b"));
    await userEvent.click(screen.getByRole("button", { name: "Lift mute" }));
    await waitFor(() => expect(requests.unmuteFinding).toHaveBeenCalled());
    expect(vi.mocked(requests.unmuteFinding).mock.calls[0][0]).toBe("sys-a");
  });
});
