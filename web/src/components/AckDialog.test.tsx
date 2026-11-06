import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import * as api from "../api/requests";
import { renderApp } from "../test/render";
import { ack, onCallFinding } from "../test/fixtures/oncall";
import { AckDialog } from "./AckDialog";
vi.mock("../api/requests", async (orig) => ({ ...(await orig<typeof import("../api/requests")>()), getLensInfo: vi.fn(), acknowledgeFinding: vi.fn(), endAcknowledgement: vi.fn() }));
const m = vi.mocked(api);
const info = { identityMode: "request" as const, version: "", cluster: "", collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: true };
beforeEach(() => { vi.resetAllMocks(); localStorage.clear(); m.getLensInfo.mockResolvedValue(info); m.acknowledgeFinding.mockResolvedValue(ack); m.endAcknowledgement.mockResolvedValue({ ...ack, endedAt: Date.now(), endedBy: "alice", endReason: "released", quiet: false }); });
it("defaults to two hours, allows an empty note, remembers the same mute identity", async () => {
  const saved = vi.fn(); renderApp(<AckDialog finding={onCallFinding} onClose={() => {}} onSaved={saved} />, "/?system=example-prod");
  expect(screen.getByLabelText("Quiet the alert")).toHaveValue("2h");
  await userEvent.type(screen.getByLabelText("Your name"), "alice");
  await userEvent.click(screen.getByRole("button", { name: "Acknowledge" }));
  await waitFor(() => expect(saved).toHaveBeenCalledWith(ack));
  expect(m.acknowledgeFinding).toHaveBeenCalledWith("example-prod", expect.objectContaining({ findingId: onCallFinding.id, note: "", ackedBy: "alice", quietMs: 7_200_000 }));
  expect(localStorage.getItem("nats-lens-name")).toBe("alice");
});
it("announces validation, associates errors and focuses the first invalid field", async () => {
  renderApp(<AckDialog finding={onCallFinding} onClose={() => {}} onSaved={() => {}} />);
  fireEvent.change(screen.getByLabelText("Note (optional)"), { target: { value: "x".repeat(501) } });
  await userEvent.click(screen.getByRole("button", { name: "Acknowledge" }));
  const note = screen.getByLabelText("Note (optional)");
  expect(note).toHaveAttribute("aria-invalid", "true"); expect(note).toHaveFocus();
  expect(document.getElementById(note.getAttribute("aria-describedby")!)).toHaveTextContent("500");
  expect(screen.getAllByRole("alert").length).toBeGreaterThan(0); expect(m.acknowledgeFinding).not.toHaveBeenCalled();
});
it("uses verified IAP identity, accepts 500 Unicode characters and until resolved", async () => {
  m.getLensInfo.mockResolvedValue({ ...info, identityMode: "iap" });
  renderApp(<AckDialog finding={onCallFinding} onClose={() => {}} onSaved={() => {}} />);
  await screen.findByText("The acknowledgement is recorded as your IAP identity.");
  expect(screen.queryByLabelText("Your name")).toBeNull();
  fireEvent.change(screen.getByLabelText("Note (optional)"), { target: { value: "🐈".repeat(500) } });
  fireEvent.change(screen.getByLabelText("Quiet the alert"), { target: { value: "resolved" } });
  await userEvent.click(screen.getByRole("button", { name: "Acknowledge" }));
  await waitFor(() => expect(m.acknowledgeFinding).toHaveBeenCalledWith("", expect.objectContaining({ ackedBy: "", quietMs: undefined, untilResolved: true })));
});
it("offers 30 minutes and keeps server errors live in the open dialog", async () => {
  m.acknowledgeFinding.mockRejectedValue(new Error("Finding has resolved")); localStorage.setItem("nats-lens-name", "alice");
  renderApp(<AckDialog finding={onCallFinding} onClose={() => {}} onSaved={() => {}} />);
  fireEvent.change(screen.getByLabelText("Quiet the alert"), { target: { value: "30m" } });
  await userEvent.click(screen.getByRole("button", { name: "Acknowledge" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Finding has resolved"); expect(screen.getByRole("dialog")).toBeInTheDocument();
  expect(m.acknowledgeFinding.mock.calls[0][1].quietMs).toBe(1_800_000);
});
it("hands back with the captured identity and records who ended it", async () => {
  localStorage.setItem("nats-lens-name", "alice"); const saved = vi.fn();
  renderApp(<AckDialog finding={{ ...onCallFinding, acknowledgement: ack }} handBack onClose={() => {}} onSaved={saved} />, "/?system=example-prod");
  await userEvent.click(screen.getByRole("button", { name: "Hand back" }));
  await waitFor(() => expect(m.endAcknowledgement).toHaveBeenCalledWith("example-prod", "a1", "alice"));
  expect(saved).toHaveBeenCalledWith(expect.objectContaining({ endReason: "released", endedBy: "alice" }));
});
it("lets Escape close without saving and restores opener focus", async () => {
  const { useState } = await import("react");
  function Host() { const [open, setOpen] = useState(false); return <><button onClick={() => setOpen(true)}>open</button><AckDialog finding={open ? onCallFinding : undefined} onClose={() => setOpen(false)} onSaved={() => {}} /></>; }
  renderApp(<Host />); await userEvent.click(screen.getByText("open"));
  fireEvent(screen.getByRole("dialog"), new Event("cancel", { bubbles: true, cancelable: true }));
  expect(screen.queryByRole("dialog")).toBeNull(); expect(screen.getByText("open")).toHaveFocus(); expect(m.acknowledgeFinding).not.toHaveBeenCalled();
});
