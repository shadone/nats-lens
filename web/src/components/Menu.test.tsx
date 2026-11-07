import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Menu } from "./Menu";

function setup(onAll = vi.fn()) {
  const picked = vi.fn();
  render(
    <>
      <Menu
        label="System: a"
        menuLabel="Systems"
        choices={[
          { key: "a", label: "a", checked: true, onSelect: () => picked("a") },
          { key: "b", label: "b", checked: false, onSelect: () => picked("b") },
        ]}
        actions={[{ key: "all", label: "All systems", onSelect: onAll }]}
      >
        a
      </Menu>
      <p>outside</p>
    </>,
  );
  return { picked, onAll, button: screen.getByRole("button", { name: "System: a" }) };
}

describe("Menu", () => {
  it("marks only the current choice, with a drawn check mark and no text", async () => {
    const { button } = setup();
    await userEvent.click(button);
    const [a, b] = screen.getAllByRole("menuitemradio");
    expect(a.querySelector(".mc svg")).not.toBeNull();
    expect(b.querySelector(".mc svg")).toBeNull();
    expect(a).toHaveTextContent(/^a$/);
  });

  it("closes on a click outside it", async () => {
    const { button } = setup();
    await userEvent.click(button);
    expect(screen.getByRole("menu")).toBeInTheDocument();
    await userEvent.click(screen.getByText("outside"));
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("reaches an action with the arrow keys, runs it and closes", async () => {
    const { button, onAll } = setup();
    await userEvent.click(button);
    await userEvent.keyboard("{ArrowDown}{ArrowDown}");
    const all = screen.getByRole("menuitem", { name: "All systems" });
    expect(all).toHaveFocus();
    expect(screen.getByRole("menu").querySelector("a")).toBeNull();
    await userEvent.keyboard("{Enter}");
    expect(onAll).toHaveBeenCalled();
    expect(screen.queryByRole("menu")).toBeNull();
    expect(button).toHaveFocus();
  });

  it("closes after a click on an action", async () => {
    const { button, onAll } = setup();
    await userEvent.click(button);
    await userEvent.click(screen.getByRole("menuitem", { name: "All systems" }));
    expect(onAll).toHaveBeenCalled();
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("keeps a handled Escape to itself", async () => {
    const outer = vi.fn();
    document.addEventListener("keydown", outer);
    const { button } = setup();
    await userEvent.click(button);
    fireEvent.keyDown(screen.getByRole("menuitemradio", { name: /a/ }), { key: "Escape" });
    document.removeEventListener("keydown", outer);
    expect(screen.queryByRole("menu")).toBeNull();
    expect(outer).not.toHaveBeenCalled();
  });
});
