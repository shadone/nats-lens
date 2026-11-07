import { readFileSync } from "node:fs";
const clientReference = readFileSync("../docs/features/client-checks.md", "utf8");
const streamReference = readFileSync("../docs/features/stream-checks.md", "utf8");
import { create } from "@bufbuild/protobuf";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Routes, Route, useNavigate } from "react-router";
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

function SwitchSystem() {
  const navigate=useNavigate();
  return <button onClick={() => navigate("/checks/consumer-009?system=example-staging&range=6h")}>Switch test system</button>;
}

// Use shipped authored paragraphs rather than a shortened mock of the rules.
function authoredReference(source: string, code: string) {
  const body = source.split(`### \`${code}\``)[1]!.split("\n### ")[0]!;
  return body.split("\n- **").slice(1).map(block => {
    const end = block.indexOf("**");
    const title = block.slice(0, end).slice(0, -1);
    return {$typeName: "natslens.v1.CheckReferenceSection" as const,
      title: title === "Problem" ? "Meaning" : title === "Left out" ? "Limitations" : title,
      text: block.slice(end + 2).trim()};
  });
}

describe("check reference routes", () => {
  it.each([
    ["client-006", "Needs event capture", "Without it nats-lens stores no `client.auth_error` event and the check stays quiet; it cannot tell \"no failed login\" from \"not captured\". The Accounts page says which it is."],
    ["client-006", "Keys changed", "A mute set on the old key no longer applies, so mute the new key again."],
    ["client-008", "Unknown versions", "A version that does not start with a number is unknown and not judged. So is nats.py's `0.0.0`, which a build without package metadata reports."],
    ["client-008", "Thresholds", "Minimum per language.\n\n| `min_python` | `python3` | nats.py | 2.9.0 |\n\nA minimum of 0 turns a language off."],
    ["consumer-015", "The limit cannot be changed in place", "nats-server refuses an update of `max_waiting`. A queue that is full by design is muted with that reason."],
  ])("renders %s %s content independently of concise meaning", async (code, title, text) => {
    const data = response();
    data.definitions = [create(CheckDefinitionSchema, {...definition, code, reference: [{$typeName: "natslens.v1.CheckReferenceSection", title: "Meaning", text: "Concise meaning."}, {$typeName: "natslens.v1.CheckReferenceSection", title, text}]})];
    api.listCheckDefinitions.mockResolvedValue(data);
    detail(code);
    const heading = await screen.findByRole("heading", {name: title, level: 3});
    expect(heading.parentElement).toHaveTextContent(text.replaceAll("`", "").replace(/\s+/g, " "));
    expect(screen.getByText("Concise meaning.")).toBeInTheDocument();
  });

  it("renders the authored client-008 minimums as a semantic table", async () => {
    const data = response();
    data.definitions = [create(CheckDefinitionSchema, { ...definition, code: "client-008", reference: authoredReference(clientReference, "client-008") })];
    api.listCheckDefinitions.mockResolvedValue(data);
    detail("client-008");
    const table = await screen.findByRole("table");
    expect(within(table).getAllByRole("columnheader").map(h => h.textContent)).toEqual(["Threshold", "Language the client sends", "Library", "Default"]);
    expect(within(table).getAllByRole("row")).toHaveLength(8);
    for (const [threshold, language, library, version] of [
      ["min_go", "go", "nats.go", "1.37.0"], ["min_nats_js", "nats.js", "nats.js on Node.js", "2.28.0"],
      ["min_python", "python3", "nats.py", "2.9.0"], ["min_java", "java", "jnats", "2.20.0"],
      ["min_dotnet", ".NET", "NATS.Net", "2.5.0"], ["min_rust", "rust", "async-nats", "0.37.0"], ["min_c", "C", "nats.c", "3.9.0"],
    ]) {
      const row = within(table).getByRole("cell", { name: threshold }).parentElement!;
      expect(within(row).getAllByRole("cell").map(c => c.textContent)).toEqual([threshold, language, library, version]);
      expect(within(row).getByText(threshold).tagName).toBe("CODE");
    }
    expect(document.body).toHaveTextContent("A minimum of 0 turns a language off.");
    expect(screen.getByRole("heading", { name: "Unknown versions", level: 3 }).parentElement).toHaveTextContent("0.0.0");
  });

  it("renders full client-007 holding and resolution rules alongside concise meaning and numeric defaults", async () => {
    const data = response();
    data.definitions = [create(CheckDefinitionSchema, { ...definition, code: "client-007", name: "Unnamed connections", entity: "client", reference: authoredReference(clientReference, "client-007"), thresholds: [{$typeName: "natslens.v1.CheckThreshold", name: "min_age", defaultValue: "10m", unit: "duration", description: "Minimum connected age before firing."}] })];
    api.listCheckDefinitions.mockResolvedValue(data);
    detail("client-007");
    await screen.findByRole("heading", {name: "Unnamed connections", level: 1});
    const rules = screen.getByRole("heading", {name: "Thresholds", level: 3}).parentElement!;
    for (const rule of ["however young", "CONNZ failed", "connz_limit", "10th such collection in a row", "resolves at once in a whole collection"]) {
      expect(rules).toHaveTextContent(rule);
    }
    expect(screen.getByText("10m")).toBeInTheDocument();
  });

  it("renders qualified stream-008 meaning without inferring rebuild history or message identity", async () => {
    const data = response();
    data.definitions = [create(CheckDefinitionSchema, { ...definition, code: "stream-008", reference: authoredReference(streamReference, "stream-008") })];
    api.listCheckDefinitions.mockResolvedValue(data);
    detail("stream-008");
    await screen.findByText(/Matching sequence boundaries and message counts do not prove identical messages/);
    expect(document.body).not.toHaveTextContent("a replica was rebuilt");
    expect(document.body).not.toHaveTextContent("holds the same messages");
    expect(document.body).toHaveTextContent("leader step-down does not guarantee repair");
  });

  it("uses the concise authored meaning in the detail", async () => {
    const data=response();
    data.definitions[0]!.reference.unshift({$typeName:"natslens.v1.CheckReferenceSection",title:"Meaning",text:"Reported creation timestamps differ; cause is unproven."});
    api.listCheckDefinitions.mockResolvedValue(data);
    detail();
    expect(await screen.findByText("Reported creation timestamps differ; cause is unproven.")).toBeInTheDocument();
    expect(screen.queryByText(definition.description)).not.toBeInTheDocument();
  });

  it("does not retain configured values while another system loads", async () => {
    const configured=response("configured"); configured.definitions[0]!.disabled=true;
    api.listCheckDefinitions.mockResolvedValueOnce(configured).mockImplementationOnce(() => new Promise(() => {}));
    renderApp(<><SwitchSystem /><Routes><Route path="/checks/:code" element={<CheckDetail />} /></Routes></>,"/checks/consumer-009?system=example-prod&range=6h");
    expect(await screen.findByText("Disabled for this system")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button",{name:"Switch test system"}));
    expect(screen.queryByText("Disabled for this system")).not.toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Loading the check reference");
    expect(api.listCheckDefinitions).toHaveBeenLastCalledWith({system:"example-staging"});
  });
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
    const heading = await screen.findByRole("heading",{name:"Check not found",level:1});
    // The page heading takes its size from .page-h like the other not-found pages.
    expect(heading.parentElement).toHaveClass("page-h");
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
