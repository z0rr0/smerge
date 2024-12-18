package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	_ "time/tzdata"

	"github.com/z0rr0/smerge/cfg"
	"github.com/z0rr0/smerge/server"
)

var (
	// Version is git version
	Version = ""
	// Revision is revision number
	Revision = ""
	// BuildDate is build date
	BuildDate = ""
	// GoVersion is runtime Go language version
	GoVersion = runtime.Version()
)

func main() {
	const name = "smerge"
	var (
		debug       bool
		showVersion bool
		configFile  = "config.json"
	)
	defer func() {
		if r := recover(); r != nil {
			slog.Error("abnormal termination", "version", Version, "error", r)
		}
	}()
	flag.BoolVar(&showVersion, "version", showVersion, "show version")
	flag.BoolVar(&debug, "debug", debug, "debug mode")
	flag.StringVar(&configFile, "config", configFile, "configuration file")
	flag.Parse()

	versionInfo := fmt.Sprintf("%v: %v %v %v %v", name, Version, Revision, GoVersion, BuildDate)
	if showVersion {
		fmt.Println(versionInfo)
		flag.PrintDefaults()
		return
	}

	config, err := cfg.New(configFile)
	if err != nil {
		slog.Error("failed to read configuration", "error", err)
		os.Exit(1)
	}

	initLogger(config.Debug || debug)
	server.Run(config)
	slog.Info("stopped")
}

func initLogger(debug bool) {
	var level = slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
}
