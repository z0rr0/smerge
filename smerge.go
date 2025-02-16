package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime/debug"
	_ "time/tzdata"

	"github.com/z0rr0/smerge/cfg"
	"github.com/z0rr0/smerge/server"
)

var (
	// BuildDate is a build date
	BuildDate = "1970-01-01T00:00:00"
)

func main() {
	const name = "SMerge"
	var (
		dev         bool
		showVersion bool
		configFile  = "config.json"
		version     string // "v0.0.0"
		revision    string // "git:0000000"
		goVersion   string // "go1.00.0"
		err         error
	)
	defer func() {
		if r := recover(); r != nil {
			slog.Error("abnormal termination", "version", version, "error", r)
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

	goVersion, version, revision, err = getVersions()
	if err != nil {
		slog.Error("failed to read versions", "error", err)
		os.Exit(2)
	}

	versionInfo := fmt.Sprintf("%v: %v %v %v %v", name, version, revision, goVersion, BuildDate)
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
	slog.Info(name, "version", version, "revision", revision, "go", goVersion, "build", BuildDate, "dev", dev)

	server.Run(config, versionInfo)
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

// getRevision returns revision hash from build settings.
func getRevision(settings []debug.BuildSetting) string {
	const (
		revisionKey     = "vcs.revision"
		unknownRevision = "unknown"
	)

	for _, setting := range settings {
		if setting.Key == revisionKey {
			return setting.Value
		}
	}

	return unknownRevision
}

// getVersions reads build information.
// It returns Go version, CSV version and hash.
// It can return an error if build information is not available.
func getVersions() (string, string, string, error) {
	const (
		revisionPrefix = "git:"
		minRevisionLen = 7 // minimal revision hash length
	)

	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return "", "", "", fmt.Errorf("failed to read build info")
	}

	revision := getRevision(buildInfo.Settings)

	if len(revision) < minRevisionLen {
		revision = "git:unknown" // for tests
	} else {
		revision = revisionPrefix + revision[:minRevisionLen]
	}

	return buildInfo.GoVersion, buildInfo.Main.Version, revision, nil
}
