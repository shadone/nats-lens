import type {Timeline} from '../api/model';
export type TimelineCaptureLimit='NATS and collector events'|'finding transitions'|'Kubernetes events'|'server lane items'|'leader elections';
/** These flags limit event inputs, not per-check state rows or the separately
 * derived window input set. A cut somewhere in the source range does not prove
 * that a particular selected group or zoom window lost members. */
export function timelineCaptureLimits(tl:Timeline,lane:'events'|'kubernetes'|'servers'|'leaders'):TimelineCaptureLimit[]{
 if(lane==='kubernetes')return tl.kubernetesTruncated?['Kubernetes events']:[];
 if(lane==='servers')return tl.serversTruncated?['server lane items']:[];
 if(lane==='leaders')return tl.leadersTruncated?['leader elections']:[];
 const out:TimelineCaptureLimit[]=[];if(tl.natsTruncated)out.push('NATS and collector events');if(tl.findingTruncated)out.push('finding transitions');return out;
}
export function captureLimitText(limits:TimelineCaptureLimit[]=[]){return limits.length?`Source Timeline input limit reached: ${limits.join('; ')}. This view may omit other observations.`:''}
export function TimelineCaptureNotice({limits}:{limits?:TimelineCaptureLimit[]}){const text=captureLimitText(limits);return text?<p role="note">{text}</p>:null}
