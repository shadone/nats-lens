package main

import (
	"flag"

	"github.com/shadone/nats-lens/internal/config"
)

// oneDatabase holds the flags of a command that reads one system's
// database: natslens sql and natslens checks run.
type oneDatabase struct {
	path, system, dataDir *string
}

func oneDatabaseFlags(fs *flag.FlagSet) oneDatabase {
	return oneDatabase{
		path:    fs.String("config", "", "configuration file whose systems and checks section apply"),
		system:  fs.String("system", "", "system whose database is read; required when the configuration lists several"),
		dataDir: fs.String("data-dir", "", "directory holding the database (default: the system's data directory)"),
	}
}

// resolve loads the configuration, when -config is set, and returns it
// with the chosen system and the directory of the database to read:
// -data-dir when set, else the system's. Without -config the one system
// is the default configuration's, so the directory is ./data.
func (o oneDatabase) resolve() (config.Config, config.System, string, error) {
	cfg := config.Default()
	if *o.path != "" {
		var err error
		if cfg, err = config.LoadFile(*o.path, cfg); err != nil {
			return cfg, config.System{}, "", err
		}
	}
	sys, err := chosenSystem(cfg, *o.system)
	if err != nil {
		return cfg, sys, "", err
	}
	dir := sys.DataDir
	if *o.dataDir != "" {
		dir = *o.dataDir
	}
	return cfg, sys, dir, nil
}
