import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";
import { ListTraversal, useListTraversal } from "./ListTraversal";

function Pages({ context, error }: { context: string; error?: unknown }) {
  const traversal = useListTraversal(context);
  return <><span data-testid="token">{traversal.token || "first"}</span><ListTraversal traversal={traversal} next="next-token" count={230} loaded={100} error={error} refresh={() => {}} /></>;
}

describe("List traversal", () => {
  it("moves to the continuation, resets filters synchronously, and refreshes from the first page", () => {
    const { rerender } = render(<Pages context="system-a/range-a/no-kv" />);
    expect(screen.getByText("Page 1 · 100 shown · 230 matches")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Next page" }));
    expect(screen.getByTestId("token")).toHaveTextContent("next-token");
    rerender(<Pages context="system-b/range-a/no-kv" />);
    expect(screen.getByTestId("token")).toHaveTextContent("first");
    rerender(<Pages context="system-a/range-a/no-kv" />);
    expect(screen.getByTestId("token")).toHaveTextContent("first");
    fireEvent.click(screen.getByRole("button", { name: "Next page" }));
    fireEvent.click(screen.getByRole("button", { name: "Refresh list" }));
    expect(screen.getByTestId("token")).toHaveTextContent("first");
  });
  it("offers a reset for an expired context and disables continuation", () => {
    render(<Pages context="a" error={new ConnectError("expired", Code.FailedPrecondition)} />);
    expect(screen.getByText(/List context expired/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled();
  });
});
