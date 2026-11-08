import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Pager } from "./Pager";

describe("pagination visibility", () => {
  it.each([0, 2])("keeps the count but hides pagination for %d single-page results", (count) => {
    render(<Pager page={1} next="" total={count} shown={count} onNext={vi.fn()} onPrev={vi.fn()} />);
    expect(screen.getByText(`${count} shown of ${count}`)).toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Pagination" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Previous" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Next" })).toBeNull();
    expect(screen.queryByText(/page 1/)).toBeNull();
  });

  it("keeps navigation on the first and last page of multiple pages", async () => {
    const next = vi.fn(), prev = vi.fn();
    const { rerender } = render(<Pager page={1} next="cursor" total={102} shown={100} onNext={next} onPrev={prev} />);
    expect(screen.getByRole("button", { name: "Previous" })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(next).toHaveBeenCalledOnce();
    rerender(<Pager page={2} next="" total={102} shown={2} onNext={next} onPrev={prev} />);
    expect(screen.getByText("2 shown of 102 · page 2")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "Previous" }));
    expect(prev).toHaveBeenCalledOnce();
  });
});
