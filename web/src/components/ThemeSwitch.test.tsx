import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ThemeSwitch } from "./ThemeSwitch";

const root = document.documentElement;

describe("ThemeSwitch", () => {
  beforeEach(() => {
    localStorage.clear();
    root.removeAttribute("data-theme");
  });
  afterEach(() => vi.restoreAllMocks());

  it("cycles system, light, dark and back, storing only a chosen theme", async () => {
    render(<ThemeSwitch />);
    const btn = screen.getByRole("button", { name: /Theme: System/ });
    await userEvent.click(btn);
    expect(root.getAttribute("data-theme")).toBe("light");
    expect(localStorage.getItem("nats-lens-theme")).toBe("light");
    expect(btn).toHaveAccessibleName(/Theme: Light/);
    await userEvent.click(btn);
    expect(root.getAttribute("data-theme")).toBe("dark");
    expect(localStorage.getItem("nats-lens-theme")).toBe("dark");
    await userEvent.click(btn);
    expect(root.hasAttribute("data-theme")).toBe(false);
    expect(localStorage.getItem("nats-lens-theme")).toBeNull();
  });

  it("starts from the stored theme", () => {
    localStorage.setItem("nats-lens-theme", "dark");
    render(<ThemeSwitch />);
    expect(screen.getByRole("button")).toHaveAccessibleName(/Theme: Dark/);
  });

  it("still switches when storage throws", async () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    render(<ThemeSwitch />);
    await userEvent.click(screen.getByRole("button", { name: /Theme: System/ }));
    expect(root.getAttribute("data-theme")).toBe("light");
  });
});
