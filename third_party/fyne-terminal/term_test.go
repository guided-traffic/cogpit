package terminal

import (
	"fmt"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	_ "fyne.io/fyne/v2/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTerminal(t *testing.T) {
	term := New()
	assert.NotNil(t, term)

	term.Refresh() // ensure visuals set up
	assert.NotNil(t, term.content)
}

func TestTerminal_CreateRenderer_KeepsContent(t *testing.T) {
	term := New()
	term.Resize(fyne.NewSize(200, 100))
	term.handleOutput([]byte("Hello"))
	assert.Equal(t, "Hello", term.Text())

	grid, cursor := term.content, term.cursor
	// Fyne destroys renderers of widgets that have not been drawn for a while
	// (e.g. a background tab) and then calls CreateRenderer again.
	term.CreateRenderer()

	assert.Same(t, grid, term.content)
	assert.Same(t, cursor, term.cursor)
	assert.Equal(t, "Hello", term.Text())
}

func TestExitCode(t *testing.T) {
	term := New()
	assert.Equal(t, term.ExitCode(), int(-1))
}

// shellTimeout bounds every wait on a local shell in these tests; a shell starts and exits within a
// few seconds even on a starved machine (2.3 s on a Linux container limited to 0.05 CPUs).
const shellTimeout = 60 * time.Second

// startLocalShell runs a local shell in term and returns once the shell's terminal is open, with the
// channel RunLocalShell's result arrives on. Cogpit patch: the tests wrote to the terminal until a
// write succeeded, but New gives a terminal a writer that discards its input until the terminal is
// open, so the first write, 50 ms after the start, always succeeded. When the terminal opened later
// than that (on a loaded runner), the input went nowhere: TestExitCode01 waited for the exit of a
// shell that never got its "exit" until the test binary timed out, and TestTerminal_Close read the
// command before it was set.
func startLocalShell(t *testing.T, term *Terminal) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- term.RunLocalShell()
	}()
	t.Cleanup(term.Close) // a failed test leaves no shell behind; closing again is harmless

	deadline := time.Now().Add(shellTimeout)
	for !isConnected(term) {
		select {
		case err := <-done:
			t.Fatalf("RunLocalShell returned before its terminal was open: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			term.Close()
			t.Fatalf("the local shell's terminal was not open after %s", shellTimeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return done
}

// isConnected reports whether term's connection is open, under the lock that open and close hold,
// so that what open set before (the input, the command) is visible to the test's goroutine.
func isConnected(term *Terminal) bool {
	term.closeLock.Lock()
	defer term.closeLock.Unlock()
	return term.connected
}

// waitShellExit waits for RunLocalShell to return, which it does once the shell has exited, and
// fails the test, hanging the shell up, if it has not after shellTimeout.
func waitShellExit(t *testing.T, term *Terminal, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(shellTimeout):
		term.Close()
		t.Fatalf("the shell did not exit within %s", shellTimeout)
	}
}

func testExitCodeN(t *testing.T, n int) {
	term := New()
	term.Resize(fyne.NewSize(45, 45))
	done := startLocalShell(t, term)

	_, err := fmt.Fprintf(term, "exit %d\n", n)
	require.NoError(t, err)
	waitShellExit(t, term, done)

	assert.Equal(t, n, term.ExitCode())
}

func TestExitCode01(t *testing.T) {
	testExitCodeN(t, 0)
	testExitCodeN(t, 1)
}

func TestTerminal_Resize(t *testing.T) {
	term := New()
	term.Resize(fyne.NewSize(45, 45))

	assert.Equal(t, uint(5), term.config.Columns)
	assert.Equal(t, uint(2), term.config.Rows)
}

func TestTerminal_Resize_Narrower(t *testing.T) {
	term := New()
	term.Resize(fyne.NewSize(85, 45))
	assert.Equal(t, uint(10), term.config.Columns)

	term.handleOutput([]byte("12345678"))
	assert.Equal(t, 8, term.cursorCol)

	// Cogpit patch: the line wraps again at the new width, the cursor after its last character.
	term.Resize(fyne.NewSize(45, 45))
	assert.Equal(t, uint(5), term.config.Columns)
	assert.Equal(t, "12345\n678", term.content.Text())
	assert.Equal(t, 1, term.cursorRow)
	assert.Equal(t, 3, term.cursorCol)

	term.handleOutput([]byte("abcdefgh"))
	assert.LessOrEqual(t, term.cursorCol, 5)
	for _, row := range term.content.Rows {
		assert.LessOrEqual(t, len(row.Cells), 5)
	}
}

func TestTerminal_AddListener(t *testing.T) {
	term := New()
	listen := make(chan Config, 1)
	term.AddListener(listen)
	assert.Equal(t, 1, len(term.listeners))

	go term.onConfigure()
	select {
	case <-listen: // passed
	case <-time.After(5 * time.Second): // Cogpit patch: a bound only a missing callback reaches
		t.Error("Failed waiting for configure callback")
	}
	term.RemoveListener(listen)
	assert.Equal(t, 0, len(term.listeners))
}

func TestTerminal_SanitizePosition(t *testing.T) {
	tests := []struct {
		name   string
		pos    fyne.Position
		width  int
		height int
		want   fyne.Position
	}{
		{
			name:   "WithinBounds",
			pos:    fyne.NewPos(2, 3),
			width:  45,
			height: 45,
			want:   fyne.NewPos(2, 3),
		},
		{
			name:   "NegativeX",
			pos:    fyne.NewPos(-1, 2),
			width:  45,
			height: 45,
			want:   fyne.NewPos(0, 2),
		},
		{
			name:   "ExceedsWidth",
			pos:    fyne.NewPos(46, 2),
			width:  45,
			height: 45,
			want:   fyne.NewPos(45, 2),
		},
		{
			name:   "NegativeY",
			pos:    fyne.NewPos(3, -1),
			width:  45,
			height: 45,
			want:   fyne.NewPos(3, 0),
		},
		{
			name:   "ExceedsHeight",
			pos:    fyne.NewPos(3, 46),
			width:  45,
			height: 45,
			want:   fyne.NewPos(3, 45),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			term := New()
			term.Resize(fyne.NewSize(float32(tt.width), float32(tt.height)))

			got := term.sanitizePosition(tt.pos)

			if *got != tt.want {
				t.Errorf("got %v, want %v", *got, tt.want)
			}
		})
	}
}
