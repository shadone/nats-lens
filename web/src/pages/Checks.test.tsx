import { create } from "@bufbuild/protobuf";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Routes, Route } from "react-router";
import { beforeEach, describe, it, expect, vi } from "vitest";
import { renderApp } from "../test/render";
import { App } from "../App";
import { lens } from "../api/client";
import { CheckDefinitionSchema, ListCheckDefinitionsResponseSchema } from "../gen/natslens/v1/checks_pb";
import { GetShellStatusResponseSchema } from "../gen/natslens/v1/systems_pb";
import { Checks, CheckDetail } from "./Checks";

vi.mock("../api/client", () => ({ lens: { listCheckDefinitions: vi.fn(), listSystems: vi.fn(), getShellStatus: vi.fn(), getLensInfo: vi.fn() } }));
const api = vi.mocked(lens);
const definition = create(CheckDefinitionSchema, { code:"consumer-009", name:"Consumer creation time differs across servers", description:"Reported timestamps alone do not prove a rebuild.", entity:"consumer", category:"agreement", severity:"info", kind:"operational", remediation:"Inspect captured lifecycle events.", thresholds:[{name:"position_tolerance",defaultValue:"100",unit:"count",description:"Allowed position difference."}], reference:[{title:"Fires when",text:"Servers report differing timestamps."},{title:"Limitations",text:"Restarting servers are left out."}] });
const another = create(CheckDefinitionSchema, { ...definition, code:"server-025", name:"Slow routes", entity:"server" });
const response = (state="demo") => create(ListCheckDefinitionsResponseSchema, { definitions:[definition,another], configurationState:state });

beforeEach(() => {
  vi.resetAllMocks();
  api.listCheckDefinitions.mockResolvedValue(response());
  api.getShellStatus.mockResolvedValue(create(GetShellStatusResponseSchema,{systems:[{system:{id:"example-prod",name:"Production"}}],defaultSystem:"example-prod",systemKnown:true}));
  api.getLensInfo.mockImplementation(() => new Promise(() => {}));
});

function detail(code="consumer-009", global="system=example-prod&range=6h&account=APP") {
  return renderApp(<Routes><Route path="/checks/:code" element={<CheckDetail />} /></Routes>,`/checks/${code}?${global}`);
}

function assertGlobal(link: HTMLElement, path: string) {
  const url = new URL(link.getAttribute("href")!,"http://example.com");
  expect(url.pathname).toBe(path);
  expect(url.searchParams.get("system")).toBe("example-prod");
  expect(url.searchParams.get("range")).toBe("6h");
  expect(url.searchParams.get("account")).toBe("APP");
}

describe("check reference routes", () => {
  it("opens the catalog route", async () => {
    renderApp(<App />, "/checks?system=example-prod&range=6h");
    expect(await screen.findByRole("heading", { name: "Checks", level: 1 })).toBeInTheDocument();
    expect(await screen.findByRole("link",{name:/consumer-009 Consumer creation/})).toBeInTheDocument();
  });
  it("searches and filters the catalog with labelled keyboard controls", async () => {
    renderApp(<Checks />, "/checks?system=example-prod&range=6h&account=APP");
    const search = await screen.findByRole("searchbox",{name:"Search checks"});
    await userEvent.click(search);
    await userEvent.keyboard("slow");
    expect(screen.getByRole("status")).toHaveTextContent("1 of 2 checks");
    expect(screen.queryByRole("link",{name:/consumer-009 Consumer/})).not.toBeInTheDocument();
    assertGlobal(screen.getByRole("link",{name:/server-025 Slow routes/}),"/checks/server-025");
    await userEvent.selectOptions(screen.getByRole("combobox",{name:"Applies to"}),"consumer");
    expect(screen.getByText("No checks match. Change the search or entity filter.")).toBeInTheDocument();
  });
  it("shows check-specific meaning, evidence, defaults and steps without invented effective values", async () => {
    detail();
    expect(await screen.findByRole("heading",{name:definition.name,level:1})).toBeInTheDocument();
    expect(screen.getByText(/Demo reference/)).toBeInTheDocument();
    expect(screen.getByText("Restarting servers are left out.")).toBeInTheDocument();
    expect(screen.getByText("Inspect captured lifecycle events.")).toBeInTheDocument();
    expect(screen.queryByText(/Enabled for this system/)).not.toBeInTheDocument();
    assertGlobal(screen.getByRole("link",{name:"Checks"}),"/checks");
    expect(api.listCheckDefinitions).toHaveBeenCalledWith({system:"example-prod"});
  });
  it("distinguishes disabled, configured overrides and defaults", async () => {
    const data=response("configured"); data.definitions[0]!.disabled=true; data.definitions[0]!.thresholds[0]!.effectiveValue="250";
    api.listCheckDefinitions.mockResolvedValue(data);
    detail();
    expect(await screen.findByText("Disabled for this system")).toBeInTheDocument();
    expect(screen.getByText("250")).toBeInTheDocument();
    expect(screen.getByText("100")).toBeInTheDocument();
    expect(screen.getByText(/do not reconstruct settings/)).toBeInTheDocument();
  });
  it("says when runtime configuration is absent", async () => {
    api.listCheckDefinitions.mockResolvedValue(response("unavailable")); detail();
    expect(await screen.findByText(/enabled state and effective values are unknown/)).toBeInTheDocument();
    expect(screen.queryByText("Enabled for this system")).not.toBeInTheDocument();
  });
  it("shows an explicit unknown-code state and return link", async () => {
    detail("unknown-999");
    expect(await screen.findByRole("heading",{name:"Check not found"})).toBeInTheDocument();
    assertGlobal(screen.getByRole("link",{name:"Browse all checks"}),"/checks");
  });
  it("opens the detail route through the shell", async () => {
    renderApp(<App />, "/checks/consumer-009?system=example-prod&range=6h");
    expect(await screen.findByRole("heading", { name:definition.name,level:1 })).toBeInTheDocument();
  });
  it("reports loading and supports retry after an error", async () => {
    api.listCheckDefinitions.mockRejectedValue(new Error("synthetic failure"));
    renderApp(<Checks />,"/checks");
    expect(screen.getByRole("status")).toHaveTextContent("Loading the check catalog");
    expect(await screen.findByRole("alert")).toHaveTextContent("synthetic failure");
    api.listCheckDefinitions.mockResolvedValue(response());
    await userEvent.click(screen.getByRole("button",{name:"Retry"}));
    expect(await screen.findByText("2 of 2 checks")).toBeInTheDocument();
  });
});
