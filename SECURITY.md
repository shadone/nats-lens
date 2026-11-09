# Security policy

## Report a vulnerability

Report a vulnerability privately through GitHub: open a [private security advisory](https://github.com/shadone/nats-lens/security/advisories/new). Do not open a public issue, discussion or pull request for it.

Include the nats-lens version, how it runs (binary, container image, the Kubernetes manifests), what an attacker can do, and the steps to reproduce it. The maintainer follows up in the advisory, where the fix and its disclosure are agreed.

## Supported versions

Security fixes go into the latest release. Upgrade to it before you report, and say in the report whether the issue is in an older release only.

## Why exposure matters

nats-lens holds credentials that other tools often do not:

- **System account credentials.** natslens connects as a user of the NATS system account, and as a user of each JetStream account it captures advisories from. Use the scoped users that [NATS permissions](docs/configuration.md#nats-permissions) describes: a system account user without permissions may also reload servers or put them into lame duck mode.
- **An API that changes what alerts.** The API has no authentication of its own. Anyone who reaches it can read every recorded server, connection and stream name, and can mute or acknowledge findings, which stops their alerts. natslens refuses to serve the API beyond loopback unless `iap_audience` is set or `allow_unauthenticated: true` says so explicitly; [Deploy](docs/deploy.md) shows a NetworkPolicy and an Identity-Aware Proxy setup that limit who reaches it.
- **Notification secrets.** Webhook URLs and bearer tokens belong in files or environment variables, not in the configuration file; [Notifications](docs/notifications.md) describes them.

A report that one of these protections does not hold is a security issue.
