package config

import "slices"

// ForSystem returns the configuration as a single-system file would give
// it for s: the global settings unchanged and every per-system setting
// taken from s. The code that runs one system reads this view, so that it
// needs no knowledge of systems. A setting added to System must be set
// here too; TestForSystemResolvesBackToTheSystem fails otherwise.
func (c Config) ForSystem(s System) Config {
	v := c
	v.Systems = nil
	v.SystemID = s.ID
	v.NATS = s.NATS
	v.Accounts = slices.Clone(s.Accounts)
	v.InventoryFile = s.InventoryFile
	v.Kubernetes = s.Kubernetes
	v.Retention = s.Retention
	v.ArchiveRetention = s.ArchiveRetention
	v.DataDir = s.DataDir
	v.ArchiveDir = s.ArchiveDir
	v.DuckDBMemory = ByteSize(s.DuckDBMemoryLimit)
	return v
}
