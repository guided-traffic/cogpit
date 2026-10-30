package main

import (
	"bytes"
	"errors"
	"io"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"

	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/mesa"
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

// Fyne's report of an OpenGL it cannot draw with goes through the standard log package, which
// slog.SetDefault routes into Cogpit's handlers, and reaches the Mesa offer once, with its reason
// and cause: this pins the line format mesa.Watch reads at the Fyne go.mod names.
func TestFyneGLFailureReachesTheMesaOffer(t *testing.T) {
	prev, prevOut, prevFlags := slog.Default(), log.Writer(), log.Flags()
	t.Cleanup(func() {
		slog.SetDefault(prev) // Go's own default handler leaves the log package's output alone
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	var got []mesa.Failure
	slog.SetDefault(slog.New(mesa.Watch(slog.NewTextHandler(io.Discard, nil), func(f mesa.Failure) { got = append(got, f) })))
	fyne.LogError("failed to initialise OpenGL", errors.New("glAccum"))
	fyne.LogError("failed to initialise OpenGL", errors.New("glAccum"))
	if len(got) != 1 || got[0] != (mesa.Failure{Reason: "failed to initialise OpenGL", Cause: "glAccum"}) {
		t.Errorf("the offer got %+v", got)
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
