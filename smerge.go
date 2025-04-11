package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"runtime/debug"
	"syscall"
	_ "time/tzdata"

	"github.com/z0rr0/smerge/cfg"
	"github.com/z0rr0/smerge/server"
)

var (
	// Version is a git version
	Version = "v0.0.0"
	// Revision is a revision number
	Revision = "git:0000000"
	// BuildDate is a build date
	BuildDate = "1970-01-01T00:00:00"
	// GoVersion is a runtime Go language version
	GoVersion = runtime.Version() // "go1.00.0"
)

func main() {
	const name = "SMerge"
	var (
		dev         bool
		showVersion bool
		configFile  = "config.json"
	)
	defer func() {
		if r := recover(); r != nil {
			slog.Error("abnormal termination", "version", Version, "error", r)
			_, writeErr := fmt.Fprintf(os.Stderr, "abnormal termination: %v\n", string(debug.Stack()))
			if writeErr != nil {
				slog.Error("failed to write stack trace", "error", writeErr)
			}
		}
	}()
	flag.BoolVar(&showVersion, "version", showVersion, "show version")
	flag.BoolVar(&dev, "dev", dev, "development mode")
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

	dev = dev || config.Debug
	initLogger(dev, os.Stdout)
	slog.Info(name, "version", Version, "revision", Revision, "go", GoVersion, "build", BuildDate, "dev", dev)

	server.Run(config, versionInfo, os.Interrupt, os.Signal(syscall.SIGTERM), os.Signal(syscall.SIGQUIT))
	slog.Info("stopped")
}

// initLogger initializes logger with debug mode and writer.
func initLogger(dev bool, w io.Writer) {
	var level = slog.LevelInfo
	if dev {
		level = slog.LevelDebug
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})))
}
