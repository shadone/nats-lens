# Commands

The `natslens` binary has these subcommands. `-h` after a command, such as `natslens demo -h`, prints its flags. [Configuration](configuration.md) lists the settings that `run` and `once` read.

| Command                              | Description                                                   |
| ------------------------------------ | ------------------------------------------------------------- |
| natslens [run] [flags]               | Takes one epoch per interval of every configured system, captures events and archives closed hours until stopped. |
| natslens notify test -config FILE [-channel NAME] [-system ID] | Sends a test to configured channels without opening a database or connecting to NATS. |
| natslens once [-system ID] [flags]   | Takes and stores a single epoch of every system, or only of the system `-system` names; no events. |
| natslens sql [-db FILE] [-config FILE] [-system ID] [-data-dir DIR] [-format table\|json] [-output FILE] QUERY | Runs trusted offline SQL. `-db` selects an existing file and is exclusive with legacy source flags. Default tables and JSON have explicit row/byte/time budgets; see the [SQL CLI reference](reference/sql-cli.md). Read-only access to the selected database is not an untrusted SQL sandbox. |
| natslens checks list [-config FILE]  | Lists every check with its entity, severity, kind, lookback and thresholds after the file's overrides, and whether it is disabled. |
| natslens checks run [-config FILE] [-system ID] [-data-dir DIR] [-epoch N] | Evaluates every check for the newest (or the given) epoch of a database no collector has open, read-only, and prints what fails; stores nothing. The database is chosen as for `natslens sql`. It runs within the system's `duckdb_memory_limit` and spill bound, derived from its share of the CPUs as `run` derives them; without `-config` that is the default limit, 60% of the cgroup limit or 1 GiB, so for a large copy pass a configuration with a larger `duckdb_memory_limit`. |
| natslens demo [-listen ADDR] [-allowed-host H] [-fixed] | Serves the API with the three demo systems, shifted to now unless `-fixed`. |
| natslens investigate methods         | Lists the bounded remote read commands; the [investigation command reference](reference/agent-investigations.md) describes them. |
| natslens acquire -socket PATH -system ID | Acquires a consistent offline dataset of one system from a running collector; see [Acquire an offline dataset](operations/acquire-offline-dataset.md). |
| natslens version                     | Prints the version. |

