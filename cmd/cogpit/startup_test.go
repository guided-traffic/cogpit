package main

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestStartUpLogsNoFyneError: nothing cogpit links asks Fyne for the current app before Run makes
// one. Package initialisation did: the sidebar's icons logged "Fyne error: Attempt to access
// current Fyne app when none is started" ten times at every start (fyneui, navIcons). A fresh
// process of this test binary initialises every package cogpit does and runs no test; this
// package must not import Fyne's test package, whose init makes an app and would hide the lines.
func TestStartUpLogsNoFyneError(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a process")
	}
	out, err := exec.Command(os.Args[0], "-test.run=^$", "-test.count=1").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if bytes.Contains(out, []byte("Fyne error")) || bytes.Contains(out, []byte("current Fyne app")) {
		t.Errorf("start-up logged:\n%s", out)
	}
}

// failingWriter fails every write, as stderr does for cogpit.exe started from the Start menu.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("the handle is invalid") }

// The log file gets every line whether stderr takes it or not, and stderr gets it too where it
// works; without a file the log goes to stderr alone.
func TestLogReachesTheFileWithoutStderr(t *testing.T) {
	var file, stderr bytes.Buffer
	slog.New(slog.NewTextHandler(logWriter(&file, failingWriter{}), nil)).Info("cogpit started")
	if !strings.Contains(file.String(), "cogpit started") {
		t.Errorf("with a failing stderr the file got %q", file.String())
	}

	file.Reset()
	slog.New(slog.NewTextHandler(logWriter(&file, &stderr), nil)).Info("cogpit started")
	if !strings.Contains(file.String(), "cogpit started") || !strings.Contains(stderr.String(), "cogpit started") {
		t.Errorf("the file got %q, stderr %q", file.String(), stderr.String())
	}

	stderr.Reset()
	slog.New(slog.NewTextHandler(logWriter(nil, &stderr), nil)).Info("cogpit started")
	if !strings.Contains(stderr.String(), "cogpit started") {
		t.Errorf("without a file stderr got %q", stderr.String())
	}
}
