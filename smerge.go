package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	_ "time/tzdata"

	"github.com/z0rr0/smerge/cfg"
	"github.com/z0rr0/smerge/server"
)

var (
	// Version is a git version
	Version = ""
	// Revision is a revision number
	Revision = ""
	// BuildDate is a build date
	BuildDate = ""
	// GoVersion is a runtime Go language version
	GoVersion = runtime.Version()
)

func main() {
	const name = "SMerge"
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

	initLogger(config.Debug || debug, os.Stdout)
	slog.Info(name, "version", Version, "revision", Revision, "go", GoVersion, "build", BuildDate)

	server.Run(config, versionInfo)
	slog.Info("stopped")
}

func initLogger(debug bool, w io.Writer) {
	var level = slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})))
}
