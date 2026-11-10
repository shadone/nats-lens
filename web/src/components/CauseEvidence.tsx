// A cause card's evidence: one line when a server stands out, and under
// "Evidence" the affected assets by server and a nats CLI command that
// checks the same condition.
import { useContext } from "react";
import type { Finding } from "../api/model";
import type { AssetPlacement } from "../api/overview";
import { cliCheck, placementSummary, serverRows, type ServerAsset } from "../lib/causeEvidence";
import { entityName, serverPath } from "../lib/view";
import { RLink } from "./common";
import { CopyCommand } from "./CopyCommand";
import { AccountLabelContext } from "./FindingParts";

/** The placement read for every card: the answer, or that it loads or failed. */
export interface PlacementRead {
  data?: Map<string, AssetPlacement>;
  loading: boolean;
  failed: boolean;
}

const STATE_WORD: Record<ServerAsset["state"], string> = {
  leader: "leader",
  in_sync: "",
  behind: "behind",
  offline: "offline",
  missing: "missing",
  disagrees: "disagrees",
  unknown: "no leader",
  server: "",
  connection: "",
};

function needsPlacement(fs: Finding[]): boolean {
  return fs.some((f) => f.entity?.kind === "stream" || f.entity?.kind === "consumer");
}

/** The sentence on what the placement shows, when a server stands out. */
export function PlacementSummary({ shown, noun, read }: { shown: Finding[]; noun: string; read: PlacementRead }) {
  if (!read.data) return null;
  const { rows } = serverRows(shown, read.data);
  const replicated = rows.flatMap((r) => r.assets.filter((a) => a.state !== "server" && a.state !== "connection").map((a) => a.finding.id));
  const text = placementSummary(rows, new Set(replicated).size, noun);
  return text ? <p className="cg-conc">{text}</p> : null;
}

/**
 * The "Evidence" disclosure: the servers with the affected assets each
 * holds, the assets no server holds, and the nats CLI check. Nothing when
 * there is neither a server to list nor a command.
 */
export function CauseEvidence({ shown, total, noun, read, system }: { shown: Finding[]; total: number; noun: string; read: PlacementRead; system: string }) {
  const label = useContext(AccountLabelContext);
  const pending = needsPlacement(shown);
  const { rows, unplaced } = serverRows(shown, read.data ?? new Map());
  const cli = cliCheck(shown, system);
  const held = new Set(rows.flatMap((r) => r.assets.map((a) => a.finding.id))).size;
  // A server's own findings list nothing beyond the servers the card names.
  const table = rows.some((r) => r.assets.some((a) => a.state !== "server"));
  if (!table && !pending && !cli) return null;
  return (
    <details className="cg-ev">
      <summary>Evidence</summary>
      {table && (
        <table className="cg-ev-t">
          <caption className="sr-only">Affected {noun} by server</caption>
          <thead>
            <tr>
              <th scope="col">Server</th>
              {held > 1 && <th scope="col">Affected</th>}
              <th scope="col">Assets on it</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.server}>
                <th scope="row" className="mono">
                  <RLink to={serverPath(r.server)}>{r.server}</RLink>
                </th>
                {held > 1 && (
                  <td className="num">
                    {r.assets.length} of {held}
                  </td>
                )}
                <td>
                  {r.assets.map((a, i) => (
                    <span key={a.finding.id} className="cg-ev-a">
                      {i > 0 && ", "}
                      <span className="mono">{a.state === "server" ? "this server" : entityName(a.finding.entity, label)}</span>
                      {STATE_WORD[a.state] && <span className={`cg-ev-s ${a.state}`}> {STATE_WORD[a.state]}</span>}
                    </span>
                  ))}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {pending && read.loading && !read.data && <p className="cg-ev-n">Reading where the assets sit…</p>}
      {pending && read.failed && !read.data && <p className="cg-ev-n">Where the assets sit could not be read.</p>}
      {read.data && unplaced.length > 0 && (
        <p className="cg-ev-n">
          No server holds {unplaced.map((f) => entityName(f.entity, label)).join(", ")} in the newest collection.
        </p>
      )}
      {total > shown.length && (
        <p className="cg-ev-n">
          Covers the {shown.length} {noun} listed above.
        </p>
      )}
      {cli && <CopyCommand label="Check it with the nats CLI" command={cli.command} button="Copy nats CLI check" note={cli.note} />}
    </details>
  );
}
