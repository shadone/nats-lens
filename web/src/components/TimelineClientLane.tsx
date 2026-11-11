import type {CSSProperties} from 'react';
import {Link} from 'react-router';
import type {Timeline} from '../api/model';
import {CLIENT_LANE_KINDS, cellProblems, cellTotal, clientCells, type ClientCell} from '../lib/clientLane';
import {hm} from '../lib/format';
import {evidenceQuery, evidenceRange} from '../lib/timelineEvidence';
import {useSystem} from '../lib/useSystem';
import {TimelineMark, timelineTime} from './TimelineMark';

const plural = (n: number, one: string, many = `${one}s`) => `${n.toLocaleString('en-US')} ${n === 1 ? one : many}`;

/** "120 connects, 3 drops": what a cell's aria label and the lane note say. */
export function clientCountsText(c: Pick<ClientCell, 'connects' | 'drops' | 'authErrors'>): string {
  const parts = [plural(c.connects, 'connect')];
  if (c.drops) parts.push(plural(c.drops, 'drop'));
  if (c.authErrors) parts.push(plural(c.authErrors, 'failed authentication'));
  return parts.join(', ');
}

function CellPreview({c}: {c: ClientCell}) {
  return <>
    <strong>Client connection counts</strong>
    <div>{timelineTime(c.from)} – {timelineTime(c.to)}</div>
    <div>Connects: {c.connects}</div>
    <div>Closed by the client: {c.closes}</div>
    <div>Drops: {c.drops}</div>
    <div>Disconnected by a server shutdown: {c.shutdowns}</div>
    <div>Failed authentications: {c.authErrors}</div>
    <div>Counts of captured client events in {plural(c.buckets, 'bucket')}, not one event. A churn summary counts each short connection it folded.</div>
  </>;
}

function CellDetails({c, readId}: {c: ClientCell; readId?: string}) {
  const system = useSystem();
  const path = (kinds: string[]) => {
    const p = evidenceQuery(system, evidenceRange(c.from, c.to));
    p.set('kind', kinds.join(','));
    p.set('show', 'all');
    if (readId !== undefined) p.set('eventReadId', readId);
    return `/events?${p}`;
  };
  return <section aria-label="Client event evidence">
    <p><Link className="x" to={path(CLIENT_LANE_KINDS)}>Open these client events</Link></p>
    {c.drops + c.shutdowns > 0 && <p><Link className="x" to={path(['client.disconnect'])}>Open the disconnects</Link> The Events page lists every disconnect with its reason; drops are those closed neither by the client nor by a server shutdown.</p>}
    {c.authErrors > 0 && <p><Link className="x" to={path(['client.auth_error'])}>Open the failed authentications</Link></p>}
  </section>;
}

/** The client lane: counts per bucket as bars, taller for more events; the
 * share of drops and failed authentications is coloured at the bar's foot. */
export function TimelineClientLane({tl, end, width}: {tl: Timeline; end: number; width: number}) {
  const cells = clientCells(tl.clients ?? [], tl.start, tl.bucketMs, tl.bucketCount, end, width);
  const max = Math.max(1, ...cells.map(cellTotal));
  const span = end - tl.start;
  return <>{cells.map((c) => {
    const problems = cellProblems(c) > 0;
    const style = {left: `${(100 * (c.from - tl.start)) / span}%`, width: `${(100 * (c.to - c.from)) / span}%`, '--h': (0.3 + 0.7 * Math.sqrt(cellTotal(c) / max)).toFixed(3)};
    return <TimelineMark key={c.key} className={`sl-density${problems ? ' drops' : ''}`} style={style as CSSProperties}
      label={`Client connections ${hm(c.from)}–${hm(c.to)}: ${clientCountsText(c)}`} preview={<CellPreview c={c}/>} details={<CellDetails c={c} readId={tl.eventReadId}/>}>
      {problems && <span className="sl-density-drop" style={{height: `max(3px, ${(100 * cellProblems(c) / cellTotal(c)).toFixed(1)}%)`}}/>}
    </TimelineMark>;
  })}</>;
}
