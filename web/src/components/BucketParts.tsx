// Cells shared by the KV and object store tables.
import type { Bucket } from "../api/buckets";
import type { PlacementStateName } from "../api/adapt-placement";
import { fmtBytes, fmtDuration } from "../lib/format";
import { LEVEL_ORDER, streamPath } from "../lib/view";
import { AccountName } from "./AccountName";
import { Dash, Num, RLink } from "./common";

/** The bucket's name, linked to its resource page while a server reports it. */
export function BucketName({ b, label }: { b: Bucket; label: (id: string, name?: string) => string }) {
  const e = b.entity;
  return (
    <td className="mono">
      {b.present ? <RLink to={streamPath(e.account, e.stream)}>{b.bucket}</RLink> : <span>{b.bucket}</span>}
      <small>
        <AccountName id={e.account} name={e.accountName} label={label(e.account, e.accountName)} />
        {b.description && ` · ${b.description}`}
      </small>
    </td>
  );
}

/** "none" when nothing expires, a dash when not reported. */
export function Ttl({ ms }: { ms: number | undefined }) {
  if (ms === undefined) return <Dash />;
  return <>{ms === 0 ? "none" : fmtDuration(ms)}</>;
}

/** Bytes stored, with the limit beneath when there is one. */
export function BytesOf({ b }: { b: Bucket }) {
  return (
    <td className="num">
      <Num v={b.bytes} fmt={fmtBytes} />
      {b.maxBytes !== undefined && b.maxBytes > 0 && <small>of {fmtBytes(b.maxBytes)}</small>}
    </td>
  );
}

/** Notes: missing first, then the given ones, then the finding codes. */
export function Notes({ b, notes }: { b: Bucket; notes: (string | false)[] }) {
  const rest = [...notes, ...b.findingCodes].filter((x): x is string => !!x).join(", ");
  return (
    <td className="notes">
      {!b.present && <span className="bad">missing</span>}
      {!b.present && rest && ", "}
      {rest}
    </td>
  );
}

/** The sort value of a bucket's status: worst first ascending. */
export function statusOrder(b: Bucket): number {
  return LEVEL_ORDER[b.status];
}

/** Every placement state among the buckets, for the legend. */
export function placementStates(bs: Bucket[]): PlacementStateName[] {
  return bs.flatMap((b) => b.placement.map((p) => p.state));
}
