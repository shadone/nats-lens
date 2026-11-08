# Investigation workflows

Status: proposed, not yet confirmed. Implementation is tracked on the NATS Lens board in Mission Control. These requirements describe the proposed direction, not shipped behavior. The [feature index](../features/README.md) describes implemented capabilities.

## Investigation experience

- Summaries explain their meaning, expose supporting evidence, and offer a useful next action.
- Chart help, inspection, range selection, and drill-down use consistent shared behavior across metrics.
- Every meaningful Timeline mark supports inspection and historical drill-down, including resolved findings.
- KV and object-store workflows use bucket names and terminology.
- Investigations preserve system/account scope, absolute time bounds, source identities, and evidence references.
- Missing measurements remain unknown; aggregate observations do not establish individual identities or causes.

The implemented remote subset is documented under [Agent investigations](../features/agent-investigations.md). Collection pinning, offline parity, dataset acquisition and dedicated Timeline-mark entry points remain proposed.

## Agent and offline access

- Agent clients reuse API semantics and provide bounded, read-only access.
- Live and offline investigation backends share commands and result interpretation; provenance and unavailable runtime data remain explicit.
- Offline datasets require consistent acquisition. Raw SQL remains an advanced offline option.

Exact command syntax, snapshot mechanism, MCP packaging, and UI layouts remain proposals. The [SQL access capability](../features/sql-access.md) documents the existing offline SQL command and its limits.
