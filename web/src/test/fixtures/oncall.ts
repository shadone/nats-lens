import type { Finding } from "../../api/model";
import type { OnCall } from "../../api/oncall";
export const onCallFinding: Finding = {
  id: "consumer-010:APP/order-archive/analytics-aggregator", checkCode: "consumer-010", checkName: "Expected consumer missing", severity: "critical", state: "firing",
  entity: { kind: "consumer", key: "APP/order-archive/analytics-aggregator", name: "analytics-aggregator", account: "APP", stream: "order-archive", consumer: "analytics-aggregator", server: "" },
  since: Date.parse("2026-10-17T09:31:00Z"), detail: "The expected durable is missing.", remediation: "Check the owning service first. Recreate the durable after checking its configuration.", relatedConfig: [], relatedConfigSource: "", transitionsLastHour: 0,
};
export const onCallCard: OnCall = { finding: onCallFinding, transitions: [], sameCheckFiring: 1, assets: [{ entity: onCallFinding.entity, findingId: onCallFinding.id, severity: "critical" }], assetsTotal: 1, now: {}, steps: [], stepsTotal: 0, stepsNote: "Before stored history.", storyA: 1_700_000_000_000, storyB: 1_700_000_600_000, playbookUrl: "https://runbooks.example.com/nats/durable-missing" };
export const ack = { id: "a1", findingId: onCallFinding.id, ackedBy: "alice@example.com", note: "Watching the upgrade", createdAt: Date.parse("2026-10-17T11:35:00Z"), quietUntil: Date.parse("2026-10-17T13:35:00Z"), quiet: true, endedBy: "", endReason: "" };
