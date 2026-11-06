// Regression probes ported from the independent M3 review.
import { useState } from "react";
import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useNavigate } from "react-router";
import { beforeEach, expect, it, vi } from "vitest";
import type { Finding } from "../api/model";
import { renderApp } from "../test/render";
import { onCallFinding, ack } from "../test/fixtures/oncall";
import { FindingAcknowledgement } from "./Acknowledgement";
vi.mock("../api/requests", async orig => ({ ...await orig<typeof import("../api/requests")>(), getLensInfo: vi.fn(async () => ({ identityMode: "request" })), endAcknowledgement: vi.fn(async () => ({ ...ack, endedAt: ack.createdAt + 1000, endedBy: "reviewer", endReason: "released", quiet: false })) }));
beforeEach(() => { localStorage.setItem("nats-lens-name", "reviewer"); });
for (const scenario of ["new episode", "resolved", "other finding", "other system"] as const) it(`clears visit hand-back receipt on ${scenario} but keeps it in the same episode`, async () => {
  let change!: (f: Finding) => void; let navigate!: ReturnType<typeof useNavigate>;
  function Host() { const [finding, setFinding] = useState<Finding>({ ...onCallFinding, acknowledgement: ack }); change = setFinding; navigate = useNavigate(); return <FindingAcknowledgement finding={finding} phone />; }
  renderApp(<Host />, "/?system=example-prod");
  await userEvent.click(screen.getByRole("button", { name: "Hand back…" }));
  await userEvent.click(screen.getByRole("button", { name: "Hand back" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  act(() => change({ ...onCallFinding }));
  expect(await screen.findByText(/Handed back by reviewer/)).toBeInTheDocument();
  act(() => {
    if (scenario === "other system") navigate("/?system=example-global&range=6h");
    else change({ ...onCallFinding, ...(scenario === "new episode" ? { since: onCallFinding.since! + 3600000 } : scenario === "resolved" ? { state: "resolved" } : { id: "other" }) });
  });
  expect(screen.queryByText(/Handed back by reviewer/)).toBeNull();
  expect(screen.getByText("No acknowledgement for this episode.")).toBeInTheDocument();
});
