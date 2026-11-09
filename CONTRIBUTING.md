# Contributing to nats-lens

Thank you for helping. This page describes how to build and test nats-lens, how to propose a change, and the repository rules that a pull request is checked against.

Ask a question in [Discussions](https://github.com/shadone/nats-lens/discussions). Report a bug or propose a feature with an [issue](https://github.com/shadone/nats-lens/issues/new/choose). Report a security issue privately, as [SECURITY.md](SECURITY.md) describes.

## Build and test

[Development](docs/development.md) lists the prerequisites (Go, Node.js, `nats-server` and the `nats` CLI, `buf`), the local three-server cluster and every make target. The commands a change usually needs:

```sh
make build
make lint
make test-short
cd web && npx tsc --noEmit && npm test
```

`go test -timeout 40m ./...` runs every Go test. The tests of `internal/checks` alone take about 13 minutes, longer than Go's default 10-minute limit. [Run the Go test tiers](docs/operations/go-test-tiers.md) lists the faster selections, and [Run repository verification](docs/operations/verification.md) the fast and full gates.

`bin/natslens demo` serves the UI with the three made-up demo systems on http://127.0.0.1:8080, without NATS.

## Propose a change

1. For a larger change, open an issue or a discussion first, so that the approach can be agreed before you write it.
2. Branch from `develop`, the default branch.
3. Write small commits with [Conventional Commits](https://www.conventionalcommits.org/) prefixes: `feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`.
4. For a fix, add a test that fails without the fix and passes with it.
5. Open a pull request against `develop` and fill in the template.

## Repository rules

These rules come from [AGENTS.md](AGENTS.md), which holds the full list. A pull request that breaks one is sent back.

- **Migrations are append-only.** Never edit a file in `internal/store/migrations/` once it has been applied anywhere: add a new migration. The `migrations` table holds each file's checksum, and natslens refuses to start on a mismatch. A new NOT NULL column on an archived table needs a default, or older archived hours no longer load.
- **Proto field numbers are permanent.** A proto change takes new field numbers; never reuse or renumber one. Run `make proto` and commit the generated code in `internal/gen` and `web/src/gen`.
- **Every check has a test.** A new check in `checks/` needs a test in `internal/checks` with one entity that fails and one that does not.
- **Feature docs follow the code.** Every user-facing change updates its page in [docs/features/](docs/features/README.md) and both sections of `docs/features/README.md`: the capabilities table and the coverage map.
- **Examples use made-up names.** Examples, tests and screenshots use the online shop of `inventory.example.yaml` and the `example.com` domain, never a real company, deployment or host.
- **UI styles use the tokens.** Font sizes, spacing and radius come from `web/src/styles/tokens.css`, and labels are in sentence case.
- **DuckDB shifts a `time.Time` parameter.** Wherever DuckDB cannot type it from a TIMESTAMP column, it shifts the value by the session time zone. Compare a TIMESTAMP column with the parameter directly (`col >= ?`), or pass Unix microseconds, and test such a query with a non-UTC `TZ`.

## AI agents

An AI coding agent working in this repository reads [AGENTS.md](AGENTS.md) first. It holds the layout, the commands and the rules above in the form agents follow.

## License

nats-lens is licensed under the GNU Affero General Public License v3.0 only (AGPL-3.0-only). Contributions are accepted under the same license: by contributing, you agree that your contribution is licensed under AGPL-3.0-only. The license text is in [LICENSE](LICENSE). No contributor license agreement is required.
