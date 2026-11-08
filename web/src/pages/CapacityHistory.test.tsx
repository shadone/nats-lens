import { create } from "@bufbuild/protobuf";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { lens } from "../api/client";
import * as requests from "../api/requests";
import { GetTimeSeriesBatchResponseSchema, SeriesSchema, Unit } from "../gen/natslens/v1/timeseries_pb";
import { prod, stream } from "../test/fixtures/capacity";
import { renderApp } from "../test/render";
import { Capacity } from "./Capacity";
vi.mock("../api/client", () => ({ lens: { getTimeSeriesBatch: vi.fn() } }));
vi.mock("../api/requests", async importOriginal => ({ ...await importOriginal<typeof requests>(), getCapacity: vi.fn(), getLensInfo: vi.fn(() => Promise.resolve({collectIntervalMs:30000, live:true, demo:true})), getShellStatus: vi.fn(() => Promise.reject(new Error("no shell"))) }));
beforeEach(() => {
 vi.clearAllMocks();
 const d = prod(); d.servers.forEach(s => {s.perDay=undefined});
 d.streams = Array.from({length:33}, (_,i) => stream(`orders-${i}`, {bytesPerDay:undefined})); d.streamsTotal=812;
 vi.mocked(requests.getCapacity).mockResolvedValue(d);
 vi.mocked(lens.getTimeSeriesBatch).mockImplementation(async req => create(GetTimeSeriesBatchResponseSchema, {results: (req.queries ?? []).map(q => ({series:create(SeriesSchema,{metric:q.metric, unit:Unit.BYTES, step:{seconds:30n},points:[{time:{seconds:1792238400n},value:40}]})}))}));
});
it("shows history without forecast eligibility, bounded resource pages and expanded charts", async () => {
 renderApp(<Capacity />, "/capacity?system=example-prod&account=APP&from=2026-10-17T12%3A00%3A00Z&to=2026-10-17T12%3A05%3A00Z");
 await screen.findByText("Total JetStream file-store usage", {selector:".lbl"});
 expect(await screen.findByRole("button",{name:"Expand history for orders-0"})).toBeInTheDocument();
 await userEvent.click(screen.getByRole("button",{name:"Expand history for orders-0"}));
 expect(screen.getByText("Observed replica bytes", {selector:".lbl"})).toBeInTheDocument();
 expect(screen.getByText(/History rows 1–16 of 33/)).toBeInTheDocument();
 await userEvent.click(screen.getByRole("button",{name:"Next resource history page"}));
 expect(screen.getByRole("link",{name:"orders-16"})).toBeInTheDocument();
 await waitFor(() => expect(vi.mocked(lens.getTimeSeriesBatch).mock.calls.length).toBeGreaterThan(1));
 for (const [req, opts] of vi.mocked(lens.getTimeSeriesBatch).mock.calls) {
  expect(req.queries!.length).toBeLessThanOrEqual(32); expect(req.system).toBe("example-prod"); expect(req.range).toMatchObject({start:{seconds:1792238400n},end:{seconds:1792238700n}}); expect(opts?.signal).toBeInstanceOf(AbortSignal);
 }
 expect(screen.getByTestId("location").textContent).toContain("account=APP");
});
