import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AccountName } from "./AccountName";

describe("AccountName", () => {
  it("shows the name and gives screen readers the ID", () => {
    render(<AccountName id="ACJQEXMP" name="APP" />);
    expect(screen.getByText("APP")).toHaveAttribute("title", "ACJQEXMP");
    expect(screen.getByText("(account ID ACJQEXMP)", { exact: false })).toHaveClass("sr-only");
  });
  it("shows the ID alone without a name", () => {
    render(<AccountName id="SYS" />);
    expect(screen.getByText("SYS")).toBeInTheDocument();
    expect(screen.queryByText(/account ID/)).toBeNull();
  });
});
