import type {CaptureGap, Range} from '../api/model';
import {hm} from '../lib/format';
import {TimelineMark, timelineTime} from './TimelineMark';

const hms = (t: number) => new Date(t).toISOString().slice(11, 19);

/** "07:30–07:31", or with seconds when both ends fall in one minute. */
export function gapSpanText(g: Range): string {
  return hm(g.start) === hm(g.end) ? `${hms(g.start)}–${hms(g.end)}` : `${hm(g.start)}–${hm(g.end)}`;
}

/** " (account APP)" or " (system account)" for a lost connection's span. */
export function gapConnectionText(g: Range & {connection?: string}): string {
  return g.connection ? (g.connection === 'sys' ? ' (system account)' : ` (account ${g.connection})`) : '';
}

/** Why a span has no events: the lane's own source, or one account's. */
function why(g: Range & {connection?: string}, lane: 'system' | 'advisory'): string {
  if (lane === 'advisory' && g.connection === 'sys') return "The system account connection was lost: meta leader elections were not captured. Other accounts' stream and consumer elections may have been.";
  if (lane === 'advisory' && g.connection) return `The account connection ${g.connection} was lost: this account's advisories were not captured. Other accounts' advisories may have been.`;
  if (lane === 'advisory') return 'nats-lens was not running.';
  return 'nats-lens was not running, or its system account connection was lost.';
}

/** A span of a lane in which events were not captured, hatched so that an
 * empty stretch does not read as a quiet one. A span narrower than the
 * hatch's minimum width is still drawn at that width. */
export function TimelineCaptureGap({gap, frac, lane}: {gap: Range | CaptureGap; frac: (t: number) => number; lane: 'system' | 'advisory'}) {
  const left = Math.max(0, frac(gap.start)), right = Math.min(1, frac(gap.end));
  const account = gapConnectionText(gap);
  // The minimum width must not push a span at the right edge out of the track.
  return <TimelineMark className="sl-gap" style={{left: `min(${(100 * left).toFixed(3)}%, calc(100% - var(--sp-1)))`, width: `${(100 * (right - left)).toFixed(3)}%`}}
    label={`Event capture stopped ${gapSpanText(gap)}${account}`}
    preview={<><strong>Event capture stopped{account}</strong><div>{timelineTime(gap.start)} – {timelineTime(gap.end)}</div>
      <div>{why(gap, lane)} Events of this span were not captured, so the lane is empty here for want of capture, not because it was quiet.</div></>}/>;
}
