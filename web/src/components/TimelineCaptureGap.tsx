import type {Range} from '../api/model';
import {hm} from '../lib/format';
import {TimelineMark, timelineTime} from './TimelineMark';

/** A span of a lane in which events were not captured, hatched so that an
 * empty stretch does not read as a quiet one. */
export function TimelineCaptureGap({gap, frac}: {gap: Range; frac: (t: number) => number}) {
  const left = Math.max(0, frac(gap.start)), right = Math.min(1, frac(gap.end));
  return <TimelineMark className="sl-gap" style={{left: `${(100 * left).toFixed(3)}%`, width: `${(100 * (right - left)).toFixed(3)}%`}}
    label={`Event capture stopped ${hm(gap.start)}–${hm(gap.end)}`}
    preview={<><strong>Event capture stopped</strong><div>{timelineTime(gap.start)} – {timelineTime(gap.end)}</div>
      <div>nats-lens was not running, or a connection that captures events was lost. Events of this span were not captured, so the lane is empty here for want of capture, not because it was quiet.</div></>}/>;
}
