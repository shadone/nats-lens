import { Code, ConnectError } from "@connectrpc/connect";
import { Findings } from "./Findings";
import { screen } from "@testing-library/react";
import { Route, Routes } from "react-router";
import { beforeEach, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import { renderApp } from "../test/render";
import { onCallFinding } from "../test/fixtures/oncall";
import { FindingByLabels } from "./FindingByLabels";
vi.mock("../api/requests", async orig => ({ ...(await orig<typeof import("../api/requests")>()), getFinding: vi.fn(), listFindings: vi.fn(), listMutes: vi.fn() }));
const m = vi.mocked(requests);
beforeEach(() => { vi.resetAllMocks(); m.getFinding.mockResolvedValue({ finding: onCallFinding, transitions: [] }); m.listFindings.mockResolvedValue({ items: [], totalCount: 0, nextPageToken: "" }); m.listMutes.mockResolvedValue({ items: [], totalCount: 0, nextPageToken: "" }); });
function show(query: string) { renderApp(<Routes><Route path="/findings/by-labels" element={<FindingByLabels />} /><Route path="/findings/:id" element={<p>Existing finding</p>} /><Route path="/findings" element={<Findings />} /></Routes>, `/findings/by-labels?system=example-prod&code=consumer-010&entity_key=${query}`); }
it("decodes urlquery spaces, literal plus, slash and colon once and replaces the label route", async () => {
  show("APP%2Forder+archive%2Fworker%3Av2%2Bblue");
  await screen.findByText("Existing finding");
  expect(m.getFinding.mock.calls[0].slice(0, 2)).toEqual(["example-prod", "consumer-010:APP/order archive/worker:v2+blue"]);
  expect(screen.getByTestId("navigation")).toHaveTextContent("REPLACE");
  expect(screen.getByTestId("location")).toHaveTextContent(`/findings/${encodeURIComponent(onCallFinding.id)}?system=example-prod`);
});
it("shows the check's Findings list with an honest swept-finding message", async () => {
  m.getFinding.mockResolvedValue({ finding: undefined, transitions: [] }); show("APP%2Fgone+stream%2Fworker%3Av2");
  expect(await screen.findByText("No finding of consumer-010 for APP/gone stream/worker:v2; it may have resolved and been swept.")).toBeInTheDocument();
  expect(m.listFindings).toHaveBeenCalledWith("example-prod", expect.anything(), expect.objectContaining({ checkCode: "consumer-010" }), expect.anything());
});

it("handles a real not_found response as a swept finding and preserves other API errors", async () => {
  m.getFinding.mockRejectedValue(new ConnectError("unknown finding", Code.NotFound)); show("APP%2Fgone");
  expect(await screen.findByText("No finding of consumer-010 for APP/gone; it may have resolved and been swept.")).toBeInTheDocument();
  expect(screen.getByTestId("location")).toHaveTextContent("/findings?check=consumer-010&system=example-prod");
});
it("does not claim a network failure is a swept finding", async () => {
  m.getFinding.mockRejectedValue(new ConnectError("offline", Code.Unavailable)); show("APP%2Fgone");
  expect(await screen.findByRole("alert")).toHaveTextContent("offline");
  expect(screen.queryByText(/may have resolved and been swept/)).toBeNull();
});
