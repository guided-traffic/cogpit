// Command cogpit-cli is Cogpit's headless command-line tool: it lists the contexts Cogpit offers,
// reads objects from a cluster through a headless engine (cache first, read-only), and bridges
// MCP over stdio for agents that speak only stdio. It is pure Go: no cgo, no Fyne.
//
// Every command dispatches the same engine commands the desktop app and MCP dispatch, with origin
// CLI (or external agent, for the MCP bridge), so the engine's rules apply: nothing that needs a
// user's confirmation runs, Secret values are never printed, and a credential plugin runs only
// once the user allowed it in Cogpit's window (architecture §7, trust-boundaries H-1).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"

	"github.com/guided-traffic/cogpit/internal/app"
	"github.com/guided-traffic/cogpit/internal/buildinfo"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/locale"
)

// Exit codes.
const (
	exitOK    = 0 // done; also a list with no rows
	exitError = 1 // the command failed: no connection, not found, refused, timed out
	exitUsage = 2 // the command line is wrong: unknown command or flag, missing argument
)

func main() {
	level := new(slog.LevelVar)
	level.Set(slog.LevelWarn)
	// Logs go to stderr only: stdout carries the output, and for `mcp` the protocol. Helm values
	// never reach the log.
	logger := slog.New(helm.RedactSDKLog(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	slog.SetDefault(logger)
	klog.SetSlogLogger(logger)
	// As in the desktop app: a panicking client-go goroutine is logged, not fatal (D-11).
	utilruntime.ReallyCrash = false
	utilruntime.PanicHandlers = append(utilruntime.PanicHandlers, func(_ context.Context, r any) {
		logger.Error("recovered panic in a client-go goroutine", "panic", fmt.Sprint(r))
	})
	// An agent host that starts `cogpit-cli mcp` may lack the shell's PATH, where credential
	// plugins live (gap H-6).
	cluster.EnsureToolPath()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	dirs, dirsErr := app.UserDirs()
	if dirsErr == nil {
		// The program folders of the app's Preferences › Kubernetes (D-120): read, never written.
		cluster.SetProgramFolders(engine.ReadProgramFolders(dirs.Config))
	}
	code := run(ctx, os.Args[1:], &env{
		stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr,
		logger: logger, level: level,
		dirs: dirs, dirsErr: dirsErr,
		// Times as the shell's locale writes them (LC_ALL, LC_TIME, LANG), as command-line tools
		// do; the desktop app reads the computer's region instead (D-31).
		timeLayout: locale.Environment(os.Getenv).Layout,
	})
	stop()
	os.Exit(code)
}

// command is one subcommand.
type command struct {
	name    string
	summary string
	run     func(ctx context.Context, args []string, e *env) int
}

func commandList() []command {
	return []command{
		{name: "contexts", summary: "List the contexts Cogpit offers, from its kubeconfig sources", run: runContexts},
		{name: "get", summary: "Read objects of one kind from a cluster's cache, as Cogpit's views show them", run: runGet},
		{name: "mcp", summary: "Serve Cogpit's MCP tools over stdio: forwarded to the running app, else a headless engine", run: runMCP},
		{name: "version", summary: "Print the version", run: runVersion},
	}
}

// run runs one command line (without the program name) and returns the exit code.
func run(ctx context.Context, args []string, e *env) int {
	if len(args) == 0 {
		printUsage(e.stderr)
		return exitUsage
	}
	switch args[0] {
	case "-h", "-help", "--help":
		printUsage(e.stdout)
		return exitOK
	case "help":
		if len(args) > 1 {
			return run(ctx, []string{args[1], "--help"}, e)
		}
		printUsage(e.stdout)
		return exitOK
	case "-version", "--version":
		return runVersion(ctx, nil, e)
	}
	for _, c := range commandList() {
		if c.name == args[0] {
			return c.run(ctx, args[1:], e)
		}
	}
	fmt.Fprintf(e.stderr, "cogpit-cli: unknown command %q\n", args[0])
	fmt.Fprintln(e.stderr, "Run 'cogpit-cli --help' for the commands.")
	return exitUsage
}

func printUsage(w writer) {
	fmt.Fprint(w, `cogpit-cli is Cogpit's headless command-line tool. contexts and get only read; mcp serves agents
under Cogpit's rules, and what changes a cluster waits for the user's confirmation in Cogpit's window.

Usage:
  cogpit-cli <command> [flags] [arguments]

Commands:
`)
	for _, c := range commandList() {
		fmt.Fprintf(w, "  %-10s %s\n", c.name, c.summary)
	}
	fmt.Fprint(w, `
Run 'cogpit-cli <command> --help' for a command's flags.

Exit codes: 0 done, 1 the command failed, 2 the command line is wrong.
`)
}

func runVersion(_ context.Context, args []string, e *env) int {
	fs := newFlags("version", "", "Prints cogpit-cli's version (a development build: dev and its commit), the Go version it was built with and the platform.")
	if code, done := fs.parse(args, e); done {
		return code
	}
	if len(fs.positional) > 0 {
		return fs.usageError(e, "version takes no arguments")
	}
	// A release: "cogpit-cli 0.7.0 (go1.27.2, darwin/arm64)", which the release pipeline checks; a
	// development build: "cogpit-cli dev 0691fb32abcd-dirty (…)" (internal/buildinfo).
	fmt.Fprintf(e.stdout, "cogpit-cli %s (%s, %s/%s)\n", buildinfo.Read(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return exitOK
}
