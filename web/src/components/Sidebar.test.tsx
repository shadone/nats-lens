import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderApp } from "../test/render";
import { Sidebar } from "./Sidebar";

vi.mock("../api/queries", async (orig) => ({ ...(await orig<typeof import("../api/queries")>()), useShellStatus: () => ({ data: { tally: { critical: 1, warning: 1175, info: 0 } } }) }));
vi.mock("../api/systems", async (orig) => ({ ...(await orig<typeof import("../api/systems")>()), useSystemBadges: () => ({ data: undefined }) }));
vi.mock("./SystemSwitcher", () => ({ SystemSwitcher: () => null }));

function rail(collapsed: boolean) {
  renderApp(<Sidebar collapsed={collapsed} onCollapse={() => {}} drawerOpen={false} onCloseDrawer={() => {}} />, "/?system=example-prod");
  return screen.getByRole("link", { name: /^Findings, 1176 firing/ });
}

describe("Sidebar badges", () => {
  it("keeps the full count in the accessible name and, collapsed, in the title", () => {
    const link = rail(true);
    expect(link).toHaveAttribute("title", "Findings, 1176 firing");
    expect(link.querySelector(".nb.critical")).not.toBeNull();
  });
  it("never shortens the count to 99+, expanded or collapsed", () => {
    rail(true);
    expect(screen.queryByText("99+")).toBeNull();
  });
  it("leaves the title off the expanded sidebar", () => {
    expect(rail(false)).not.toHaveAttribute("title");
  });
});
