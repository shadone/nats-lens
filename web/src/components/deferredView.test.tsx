import { act, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { deferredView } from "./deferredView";
import { RouteBoundary } from "./RouteBoundary";

it("renders the latest scope when code resolves and a warm view synchronously", async () => {
  let resolve!: (value: React.ComponentType<{ scope: string }>) => void;
  const load = vi.fn(() => new Promise<React.ComponentType<{ scope: string }>>(r => { resolve = r; }));
  const View = deferredView(load);
  const first = render(<View scope="earlier" />);
  expect(screen.getByRole("status")).toHaveTextContent("Loading page…");
  first.rerender(<View scope="current" />);
  await act(async () => resolve(({ scope }) => <p>{scope}</p>));
  expect(screen.getByText("current")).toBeVisible();
  expect(screen.queryByText("earlier")).toBeNull();
  first.unmount();
  render(<View scope="next visit" />);
  expect(screen.getByText("next visit")).toBeVisible();
  expect(screen.queryByRole("status")).toBeNull();
  expect(load).toHaveBeenCalledTimes(1);
});

it("shares code in flight without retaining data or updating an unmounted route", async () => {
  let resolve!: (value: React.ComponentType<{ scope: string }>) => void;
  const load = vi.fn(() => new Promise<React.ComponentType<{ scope: string }>>(r => { resolve = r; }));
  const View = deferredView(load);
  const first = render(<View scope="old system" />);
  first.unmount();
  render(<View scope="new system" />);
  await act(async () => resolve(({ scope }) => <p>{scope}</p>));
  expect(screen.getByText("new system")).toBeVisible();
  expect(screen.queryByText("old system")).toBeNull();
  expect(load).toHaveBeenCalledTimes(1);
});

it("contains an import failure and keeps the exact scoped native reload URL", async () => {
  vi.spyOn(console, "error").mockImplementation(() => {});
  const View = deferredView(async () => { throw new Error("internal module address"); });
  render(<><nav>Navigation remains</nav><RouteBoundary href="/topology?system=shop&range=6h#routes"><View /></RouteBoundary></>);
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not load this page");
  expect(screen.getByRole("link", { name: "Reload page" })).toHaveAttribute("href", "/topology?system=shop&range=6h#routes");
  expect(screen.getByRole("navigation")).toBeVisible();
  expect(screen.queryByText("internal module address")).toBeNull();
});
