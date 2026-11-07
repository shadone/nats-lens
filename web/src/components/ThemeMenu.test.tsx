import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ThemeMenu } from "./ThemeMenu";

const root = document.documentElement;

describe("ThemeMenu", () => {
  beforeEach(() => {
    localStorage.clear();
    root.removeAttribute("data-theme");
  });
  afterEach(() => vi.restoreAllMocks());

  it("is one button naming the current theme that opens Light, Dark and System", async () => {
    render(<ThemeMenu />);
    const btn = screen.getByRole("button", { name: "Theme: System" });
    expect(btn).toHaveAttribute("aria-haspopup", "menu");
    expect(btn).toHaveAttribute("aria-expanded", "false");
    await userEvent.click(btn);
    expect(btn).toHaveAttribute("aria-expanded", "true");
    const items = screen.getAllByRole("menuitemradio");
    expect(items.map((i) => i.textContent)).toEqual(["Light", "Dark", "System"]);
    expect(screen.getByRole("menuitemradio", { name: /System/ })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByRole("menuitemradio", { name: /Light/ })).toHaveAttribute("aria-checked", "false");
    expect(screen.getByRole("menuitemradio", { name: /System/ })).toHaveFocus();
  });

  it("sets data-theme and stores a chosen theme; System removes both", async () => {
    render(<ThemeMenu />);
    await userEvent.click(screen.getByRole("button", { name: "Theme: System" }));
    await userEvent.click(screen.getByRole("menuitemradio", { name: /Dark/ }));
    expect(root.getAttribute("data-theme")).toBe("dark");
    expect(localStorage.getItem("nats-lens-theme")).toBe("dark");
    expect(screen.queryByRole("menu")).toBeNull();
    const btn = screen.getByRole("button", { name: "Theme: Dark" });
    expect(btn).toHaveFocus();
    await userEvent.click(btn);
    await userEvent.click(screen.getByRole("menuitemradio", { name: /System/ }));
    expect(root.hasAttribute("data-theme")).toBe(false);
    expect(localStorage.getItem("nats-lens-theme")).toBeNull();
  });

  it("moves with the arrow keys and closes on Escape", async () => {
    render(<ThemeMenu />);
    const btn = screen.getByRole("button", { name: "Theme: System" });
    await userEvent.click(btn);
    await userEvent.keyboard("{ArrowDown}");
    expect(screen.getByRole("menuitemradio", { name: /Light/ })).toHaveFocus();
    await userEvent.keyboard("{Enter}");
    expect(root.getAttribute("data-theme")).toBe("light");
    await userEvent.click(screen.getByRole("button", { name: "Theme: Light" }));
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("menu")).toBeNull();
    expect(screen.getByRole("button", { name: "Theme: Light" })).toHaveFocus();
  });

  it("starts from the stored theme", () => {
    localStorage.setItem("nats-lens-theme", "dark");
    render(<ThemeMenu />);
    expect(screen.getByRole("button")).toHaveAccessibleName("Theme: Dark");
  });

  it("still switches when storage throws", async () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    render(<ThemeMenu />);
    await userEvent.click(screen.getByRole("button", { name: "Theme: System" }));
    await userEvent.click(screen.getByRole("menuitemradio", { name: /Light/ }));
    expect(root.getAttribute("data-theme")).toBe("light");
  });
});
