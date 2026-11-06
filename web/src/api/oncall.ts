// Present facts remain optional: an absent observation is never a zero.
import type { GetOnCallResponse } from "../gen/natslens/v1/oncall_pb";
import { entity, ms, severity, window } from "./adapt-common";
import { finding, transition } from "./adapt-findings";
import { step, type ReplayStep } from "./adapt-replay";
import type { Entity, Finding, SeverityLevel, Transition, Window } from "./model";
export interface OnCallAsset { entity?: Entity; findingId: string; severity: SeverityLevel; replicas?: number; online?: number; leader?: string; answering?: boolean }
export interface OnCallNow { epochAt?: number; serversExpected?: number; serversAnswering?: number; openWindow?: Window; deliveredPerSecond?: number; streamMsgsPerSecond?: number }
export interface OnCall {
  finding?: Finding; transitions: Transition[]; sameCheckFiring: number; assets: OnCallAsset[]; assetsTotal: number; now: OnCallNow;
  steps: ReplayStep[]; stepsTotal: number; stepsNote: string; storyA?: number; storyB?: number; contextWindow?: Window; playbookUrl: string;
}
export function onCall(r: GetOnCallResponse): OnCall {
  const n = r.now;
  return { finding: r.finding ? finding(r.finding) : undefined, transitions: r.transitions.map(transition), sameCheckFiring: r.sameCheckFiring,
    assets: r.assets.map(a => ({ entity: entity(a.entity), findingId: a.findingId, severity: severity(a.severity), replicas: a.replicas, online: a.online, leader: a.leader, answering: a.answering })), assetsTotal: r.assetsTotal,
    now: n ? { epochAt: ms(n.epochAt), serversExpected: n.serversExpected, serversAnswering: n.serversAnswering, openWindow: n.openWindow ? window(n.openWindow) : undefined, deliveredPerSecond: n.deliveredPerSecond, streamMsgsPerSecond: n.streamMsgsPerSecond } : {},
    steps: r.steps.map(step), stepsTotal: r.stepsTotal, stepsNote: r.stepsNote, storyA: ms(r.storyA), storyB: ms(r.storyB), contextWindow: r.contextWindow ? window(r.contextWindow) : undefined, playbookUrl: r.playbookUrl };
}
