# nats-lens documentation

This folder holds the documentation for running, configuring and developing nats-lens. Start with the [project README](../README.md) for what nats-lens is and how to try it.

## Run and configure

| Page | What it covers |
| --- | --- |
| [Configuration](configuration.md) | Every key and flag, shutdown timing, account connections, several systems, NATS permissions |
| [Deploy](deploy.md) | The container image, the Kubernetes manifests, memory and volume size, behaviour while running |
| [Kubernetes watcher](kubernetes.md) | The pod and node changes the optional watcher records |
| [Checks](checks.md) | How a check works, threshold overrides, site-specific remediation, the inventory, the list of checks |
| [Notifications](notifications.md) | Webhook and Slack-compatible channels, the test command, the webhook body |
| [Metrics](metrics.md) | The `/metrics` series and example alert rules for Prometheus and Cloud Monitoring |
| [Commands](commands.md) | The `natslens` subcommands |

## Understand

| Page | What it covers |
| --- | --- |
| [Features](features/README.md) | One page per capability, with its status |
| [Components](components.md) | The collector, events, storage, archive, API and UI |
| [Database](database.md) | Where the data lives, the tables and the event kinds |
| [Design](design.md) | The decisions behind nats-lens |

## Reference

| Page | What it covers |
| --- | --- |
| [Offline SQL CLI](reference/sql-cli.md) | `natslens sql` sources, formats and limits |
| [Investigation commands](reference/agent-investigations.md) | `natslens investigate` profiles and output |
| [Offline dataset reference](reference/offline-datasets.md) | The manifest and database an offline dataset holds |
| [Storage comparison API](reference/storage-change.md) | `GetStorageChange`: storage compared between retained collections |
| [Metric observation summaries](reference/metric-observations.md) | Pre-bucket statistics of a metric |
| [Global metric rankings](reference/metric-contributors.md) | `ListMetricContributors`: servers, streams or consumers ranked by a metric |
| [Retained finding occurrences](reference/finding-occurrences.md) | A finding's past firing episodes |
| [Timeline event evidence](reference/timeline-event-evidence.md) | The events behind a timeline mark |
| [Stream and consumer list pagination](reference/entity-list-pagination.md) | Bounded pages of `ListStreams` and `ListConsumers` |

## Operations

| Page | What it covers |
| --- | --- |
| [Recover a database](operations/database-recovery.md) | The procedure after a database index failure |
| [Acquire an offline dataset](operations/acquire-offline-dataset.md) | Copying one system to a private dataset while collection continues |
| [Investigate with an agent](agents/investigate.md) | A playbook for agents gathering evidence through the API |

## Development

| Page | What it covers |
| --- | --- |
| [Development](development.md) | Prerequisites, a local cluster, make targets |
| [Run repository verification](operations/verification.md) | The fast and full verification gates |
| [Run the Go test tiers](operations/go-test-tiers.md) | Test selectors, dependencies and limits |
| [Build with verified assets](operations/build-reuse.md) | How `make build` reuses web assets |
| [Measure web loading](operations/measure-web-loading.md) | Bundle sizes and browser cost |
