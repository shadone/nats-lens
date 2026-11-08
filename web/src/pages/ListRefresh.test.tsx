import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, useNavigate } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";
import * as requests from "../api/requests";
import { GlobalQueryProvider } from "../lib/useGlobalQuery";
import { Streams } from "./Streams";
import { Consumers } from "./Consumers";

vi.mock("../api/requests", () => ({ listConsumers: vi.fn(), listStreams: vi.fn(), getLensInfo: vi.fn(() => new Promise(() => {})) }));
let go: ReturnType<typeof useNavigate>;
function Nav() { go = useNavigate(); return null; }
const clients: QueryClient[] = [];
function mount(kind: "streams" | "consumers") {
 const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 10000, refetchOnWindowFocus: false } } });
 clients.push(qc);
 render(<QueryClientProvider client={qc}><MemoryRouter initialEntries={[`/${kind}?system=sys-a&account=APP&range=1h`]}><GlobalQueryProvider>{kind === "streams" ? <Streams/> : <Consumers/>}<Nav/></GlobalQueryProvider></MemoryRouter></QueryClientProvider>);
}
const streamPage = (token: string) => ({streams: [], totals: {bytes: 0, durableConsumers: 0, ephemeralConsumers: 0}, totalCount: 200, nextPageToken: token});
const consumerPage = (token: string) => Object.assign([], {totalCount: 200, nextPageToken: token});
beforeEach(() => { vi.mocked(requests.listStreams).mockReset().mockResolvedValue(streamPage("") as never); vi.mocked(requests.listConsumers).mockReset(); });
afterEach(() => { for (const c of clients.splice(0)) c.clear(); });

describe.each(["streams", "consumers"] as const)("%s traversal reset with production cache freshness", (kind) => {
 const calls = () => kind === "streams" ? vi.mocked(requests.listStreams) : vi.mocked(requests.listConsumers);
 function setup(evicted = false, delayed = false) {
  let first = 0;
  let release: () => void = () => {};
  if (kind === "streams") vi.mocked(requests.listStreams).mockImplementation(async (_sys, _spec, _kv, _signal, opts) => {
   if (opts?.pageToken) { if (evicted) throw new ConnectError("evicted", Code.FailedPrecondition); return streamPage("") as never; }
   first++; if (delayed && first === 2) await new Promise<void>(r => {release = r;});
   return streamPage(`context-${first}`) as never;
  });
  else vi.mocked(requests.listConsumers).mockImplementation(async (_sys, _spec, _filters, _signal, opts) => {
   if (opts?.pageToken) { if (evicted) throw new ConnectError("evicted", Code.FailedPrecondition); return consumerPage(""); }
   first++; if (delayed && first === 2) await new Promise<void>(r => {release = r;});
   return consumerPage(`context-${first}`);
  });
  return () => release();
 }
 for (const evicted of [false, true]) it(`requests a fresh scoped first page after ${evicted ? "eviction" : "Next"}`, async () => {
  setup(evicted); mount(kind);
  await waitFor(() => expect(screen.getByRole("button", {name:"Next page"})).toBeEnabled());
  fireEvent.click(screen.getByRole("button", {name:"Next page"}));
  await waitFor(() => expect(calls()).toHaveBeenCalledTimes(2));
  if (evicted) await screen.findByText(/List context expired/);
  await waitFor(() => expect(screen.getByRole("button", {name:"Refresh list"})).toBeEnabled());
  fireEvent.click(screen.getByRole("button", {name:"Refresh list"}));
  await waitFor(() => expect(calls()).toHaveBeenCalledTimes(3));
  await waitFor(() => expect(screen.getByRole("button", {name:"Next page"})).toBeEnabled());
  expect(calls().mock.calls.map(c => [c[0], c[4]?.pageToken])).toEqual([["sys-a", ""], ["sys-a", "context-1"], ["sys-a", ""]]);
  expect(screen.getByText(/Page 1/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", {name:"Next page"}));
  await waitFor(() => expect(calls()).toHaveBeenCalledTimes(4));
  expect(calls().mock.calls[3][4]?.pageToken).toBe("context-2");
 });
 it("does not navigate using a cached old token while refresh is delayed, and isolates a scope change", async () => {
  const release = setup(false, true); mount(kind);
  await waitFor(() => expect(screen.getByRole("button", {name:"Next page"})).toBeEnabled());
  fireEvent.click(screen.getByRole("button", {name:"Next page"}));
  await waitFor(() => expect(calls()).toHaveBeenCalledTimes(2));
  await waitFor(() => expect(screen.getByRole("button", {name:"Refresh list"})).toBeEnabled());
  fireEvent.click(screen.getByRole("button", {name:"Refresh list"}));
  await waitFor(() => expect(calls()).toHaveBeenCalledTimes(3));
  expect(screen.getByRole("button", {name:"Next page"})).toBeDisabled();
  await act(async () => go(`/${kind}?system=sys-b&account=OTHER&range=24h&${kind === "streams" ? "kv=1" : "ephemeral=1&stream=OTHER/jobs"}`));
  await waitFor(() => expect(calls()).toHaveBeenCalledTimes(4));
  await act(async () => release());
  expect(calls().mock.calls.map(c => [c[0], c[4]?.pageToken])).toEqual([["sys-a", ""], ["sys-a", "context-1"], ["sys-a", ""], ["sys-b", ""]]);
  expect(screen.getByText(/Page 1/)).toBeInTheDocument();
 });
});
