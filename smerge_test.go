package main

import (
	"bytes"
	"flag"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestInitLogger(t *testing.T) {
	tests := []struct {
		name     string
		debug    bool
		wantText string
	}{
		{
			name:     "debug mode",
			debug:    true,
			wantText: "DEBUG",
		},
		{
			name:     "info mode",
			debug:    false,
			wantText: "INFO",
		},
	}

	for i := range tests {
		tc := tests[i]
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			initLogger(tc.debug, &buf)

			slog.Debug("debug message")
			slog.Info("info message")

			output := buf.String()
			if tc.debug {
				if !strings.Contains(output, "debug message") {
					t.Errorf("debug message not found in debug mode: %q", output)
				}
				return
			}

			if strings.Contains(output, "debug message") {
				t.Errorf("debug message found in info mode")
			}
		})
	}
}

func TestMainVersion(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"cmd", "-version"}

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	os.Stdout = w
	// clear default flag set
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	main()

	// restore stdout
	if err = w.Close(); err != nil {
		t.Errorf("failed to close pipe: %v", err)
	}
	os.Stdout = oldStdout

	var buf bytes.Buffer
	if _, err = io.Copy(&buf, r); err != nil {
		t.Errorf("failed to read from pipe: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "smerge:") {
		t.Errorf("version output doesn't contain expected text: %s", output)
	}
}
