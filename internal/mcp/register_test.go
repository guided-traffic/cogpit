package mcp

import (
	"encoding/base64"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/guided-traffic/cogpit/internal/testkit/prereq"
)

// TestClaudeAddStdioCommand: the registration names the bundled cogpit-cli by its absolute path,
// after replacing an earlier registration; a path a pasted command should not carry gives none.
func TestClaudeAddStdioCommand(t *testing.T) {
	cli, quiet := "/Applications/Cogpit.app/Contents/MacOS/cogpit-cli", ">/dev/null 2>&1"
	if runtime.GOOS == goosWindows {
		cli, quiet = `C:\Program Files\Cogpit\cogpit-cli.exe`, "*> $null" // PowerShell's
	}
	if got, want := ClaudeAddStdioCommand(cli), claudeAddStdio(terminalShell(), cli); got != want || !strings.Contains(got, " cogpit "+quiet+"; ") {
		t.Errorf("ClaudeAddStdioCommand(%q):\n got %s\nwant %s", cli, got, want)
	}
	for _, cli := range []string{
		"Cogpit.app/Contents/MacOS/cogpit-cli",
		"",
		"/Applications/Co\ngpit.app/Contents/MacOS/cogpit-cli",
		"/Applications/Co\x1b]0;x\agpit.app/Contents/MacOS/cogpit-cli",
		`C:\Program Files\Co` + "\n" + `gpit\cogpit-cli.exe`,
	} {
		if got := ClaudeAddStdioCommand(cli); got != "" {
			t.Errorf("ClaudeAddStdioCommand(%q) = %s, want none", cli, got)
		}
	}
}

// The command for each shell, on every system: a POSIX shell's on macOS and Linux, PowerShell's on
// Windows, whose terminal knows no /dev/null.
func TestClaudeAddStdioCommandForEachShell(t *testing.T) {
	for _, tc := range []struct {
		sh        pasteShell
		cli, want string
	}{
		{posixShell, "/Applications/Cogpit.app/Contents/MacOS/cogpit-cli",
			"claude mcp remove --scope user cogpit >/dev/null 2>&1; " +
				"claude mcp add --scope user cogpit -- /Applications/Cogpit.app/Contents/MacOS/cogpit-cli mcp"},
		{posixShell, "/Users/a b/Apps/Cogpit.app/Contents/MacOS/cogpit-cli",
			"claude mcp remove --scope user cogpit >/dev/null 2>&1; " +
				"claude mcp add --scope user cogpit -- '/Users/a b/Apps/Cogpit.app/Contents/MacOS/cogpit-cli' mcp"},
		{powerShell, `C:\Users\a\AppData\Local\Programs\Cogpit\cogpit-cli.exe`,
			"claude mcp remove --scope user cogpit *> $null; " +
				`claude mcp add --scope user cogpit -- C:\Users\a\AppData\Local\Programs\Cogpit\cogpit-cli.exe mcp`},
		{powerShell, `C:\Program Files\Cogpit\cogpit-cli.exe`,
			"claude mcp remove --scope user cogpit *> $null; " +
				`claude mcp add --scope user cogpit -- 'C:\Program Files\Cogpit\cogpit-cli.exe' mcp`},
		{powerShell, `C:\Users\O'Brien\Cogpit\cogpit-cli.exe`,
			"claude mcp remove --scope user cogpit *> $null; " +
				`claude mcp add --scope user cogpit -- 'C:\Users\O''Brien\Cogpit\cogpit-cli.exe' mcp`},
	} {
		if got := claudeAddStdio(tc.sh, tc.cli); got != tc.want {
			t.Errorf("for %q:\n got %s\nwant %s", tc.cli, got, tc.want)
		}
	}
}

// TestShellQuoteRoundTrips: a POSIX shell reads every quoted path back as the same one word,
// whatever it holds, and expands nothing in it.
func TestShellQuoteRoundTrips(t *testing.T) {
	sh := prereq.LookPath(t, "sh") // on Windows MSYS2's, as CI's Windows jobs run in it
	for _, s := range []string{
		"/Applications/Cogpit.app/Contents/MacOS/cogpit-cli",
		"/Users/a b/My Apps/Cogpit.app/Contents/MacOS/cogpit-cli",
		"/Volumes/it's/Cogpit.app",
		`/tmp/$HOME/$(id)/` + "`id`" + `/"q"/\n/!x/*/?/[a]/~/;&|<>(){}#`,
		"/tmp/-n",
		"/tmp/ünïcode/Cögpit.app",
	} {
		out, err := exec.Command(sh, "-c", "printf '%s|' "+shellQuote(s)).Output() // #nosec G204 -- the test's own words
		if err != nil {
			t.Fatalf("sh on %q: %v", s, err)
		}
		if got := string(out); got != s+"|" {
			t.Errorf("shellQuote(%q) = %s reads back as %q", s, shellQuote(s), got)
		}
	}
	if got := shellQuote("/Applications/Cogpit.app/Contents/MacOS/cogpit-cli"); got != "/Applications/Cogpit.app/Contents/MacOS/cogpit-cli" {
		t.Errorf("a plain path is quoted: %s", got)
	}
}

// TestPowerShellQuoteRoundTrips: PowerShell (Windows PowerShell, or pwsh where it is installed)
// reads every quoted path back as the same one word, whatever it holds, and expands nothing in it;
// a plain path stays bare.
func TestPowerShellQuoteRoundTrips(t *testing.T) {
	ps, err := exec.LookPath("powershell")
	if err != nil {
		ps, err = exec.LookPath("pwsh")
	}
	if err != nil && runtime.GOOS != goosWindows {
		// CI's Windows jobs run this; a Linux or macOS runner need not have pwsh.
		t.Skip("no PowerShell")
	}
	prereq.Need(t, err == nil, "Windows PowerShell on PATH")
	words := []string{
		`C:\Users\a\AppData\Local\Programs\Cogpit\cogpit-cli.exe`,
		`C:\Program Files\Cogpit\cogpit-cli.exe`,
		`C:\Users\O'Brien\it’s ‘x’ ‚y‛\cogpit-cli.exe`,
		`C:\tmp\$HOME\$(whoami)\` + "`n" + `\%PATH%\;&|<>(){}#@,[a]\~\-n\cogpit-cli.exe`,
		`C:\Users\ünïcode\Cögpit\cogpit-cli.exe`,
		`\\server\share\cogpit-cli.exe`,
	}
	// The script goes in as UTF-16 (-EncodedCommand), so no command line parses it on the way, and
	// its output comes back as UTF-8.
	script := "[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false; "
	for _, w := range words {
		script += "Write-Output " + powerShellQuote(w) + "; "
	}
	u := utf16.Encode([]rune(script))
	b := make([]byte, 0, 2*len(u))
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	out, err := exec.Command(ps, "-NoProfile", "-NonInteractive", "-EncodedCommand", // #nosec G204 -- the test's own words
		base64.StdEncoding.EncodeToString(b)).Output()
	if err != nil {
		t.Fatalf("%s: %v", ps, err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\r\n"), "\n")
	for i, w := range words {
		if i >= len(lines) || strings.TrimSuffix(lines[i], "\r") != w {
			t.Errorf("powerShellQuote(%q) = %s reads back as %q", w, powerShellQuote(w), lines)
		}
	}
	if got := powerShellQuote(words[0]); got != words[0] {
		t.Errorf("a plain path is quoted: %s", got)
	}
}
