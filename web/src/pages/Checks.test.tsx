import { screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import { renderApp } from "../test/render";
import { App } from "../App";

describe("check reference routes", () => {
  it("opens the catalog route", async () => {
    renderApp(<App />, "/checks?system=example-prod&range=6h");
    expect(await screen.findByRole("heading", { name: "Checks", level: 1 })).toBeInTheDocument();
  });
});
