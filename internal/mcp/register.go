package mcp

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
)

// ClaudeAddStdioCommand returns a ready-to-paste shell command that registers the stdio bridge at
// cli, the cogpit-cli an app bundle carries (Cogpit.app/Contents/MacOS/cogpit-cli), with Claude
// Code for every directory (user scope), replacing an earlier registration such as
// ClaudeAddCommand's:
//
//	claude mcp remove --scope user cogpit >/dev/null 2>&1; claude mcp add --scope user cogpit -- /Applications/Cogpit.app/Contents/MacOS/cogpit-cli mcp
//
// Unlike ClaudeAddCommand it carries no port and no token: Claude Code starts `cogpit-cli mcp`,
// which reads the running app's endpoint and token files itself (architecture §7), so the command
// is run once and the registration follows every restart of Cogpit. The command is written for the
// shell this system's terminal runs (terminalShell): a POSIX shell, or PowerShell on Windows; the
// path is quoted for it when it needs it (a space, a quote, a $). It returns "" for a path that is
// not absolute or holds a control character, which no pasted command should carry; the caller then
// offers ClaudeAddCommand.
func ClaudeAddStdioCommand(cli string) string {
	if !filepath.IsAbs(cli) || strings.ContainsFunc(cli, unicode.IsControl) {
		return ""
	}
	return claudeAddStdio(terminalShell(), cli)
}

// claudeAddStdio is ClaudeAddStdioCommand's command for the shell sh.
func claudeAddStdio(sh pasteShell, cli string) string {
	return fmt.Sprintf(`claude mcp remove --scope user %[1]s %[3]s; `+
		`claude mcp add --scope user %[1]s -- %[2]s mcp`, serverName, sh.quote(cli), sh.quiet)
}

// pasteShell is a shell a ready-to-paste command is written for. Both shells run commands joined
// by ; one after the other, Windows PowerShell 5.1 included, which knows neither && nor ||.
type pasteShell struct {
	quiet string              // the redirection that discards a command's output and errors
	quote func(string) string // s as one word
}

var (
	// posixShell is sh, bash and zsh: macOS's and Linux's terminals.
	posixShell = pasteShell{quiet: ">/dev/null 2>&1", quote: shellQuote}
	// powerShell is PowerShell, the shell Windows Terminal opens: *> sends every stream to $null.
	powerShell = pasteShell{quiet: "*> $null", quote: powerShellQuote}
)

// terminalShell is the shell a pasted command meets on this system.
func terminalShell() pasteShell {
	if runtime.GOOS == goosWindows {
		return powerShell
	}
	return posixShell
}

// shellQuote returns s as one word of a POSIX shell (sh, bash, zsh): unchanged when every
// character is one no shell treats specially, else in single quotes, inside which nothing is
// expanded (no $, no backquote, no zsh history !), each single quote written as a closing quote, a
// backslash-escaped quote and an opening quote.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsFunc(s, func(r rune) bool { return !shellSafe(r) }) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shellSafe reports whether r needs no quoting anywhere in a word (Python's shlex.quote keeps the
// same set; = and % only matter at a word's start, and a path starts with /).
func shellSafe(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	return strings.ContainsRune("@%+=:,./-_", r)
}

// powerShellQuote returns s as one word of PowerShell: unchanged when every character is one
// PowerShell reads as part of a bare word, else in single quotes, inside which nothing is expanded
// (no $, no backtick escape), each single quote doubled. PowerShell takes the typographic single
// quotes (‘ ’ ‚ ‛) for single quotes too, so they are doubled the same way.
func powerShellQuote(s string) string {
	if s != "" && !strings.ContainsFunc(s, func(r rune) bool { return !powerShellSafe(r) }) {
		return s
	}
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		if strings.ContainsRune("'‘’‚‛", r) {
			b.WriteRune(r)
		}
		b.WriteRune(r)
	}
	b.WriteByte('\'')
	return b.String()
}

// powerShellSafe reports whether r needs no quoting anywhere in a word that starts with a drive
// letter or a backslash, as an absolute Windows path does; a comma, for one, would make a list.
func powerShellSafe(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	return strings.ContainsRune(`\/:._-`, r)
}
