// Command cogpit is the Cogpit desktop app: a native Kubernetes IDE.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sync"
	"time"

	// Cron Jobs' Next execution reads spec.timeZone; Windows has no zone database of its own.
	_ "time/tzdata"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"

	"github.com/guided-traffic/cogpit/internal/app"
	"github.com/guided-traffic/cogpit/internal/buildinfo"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/locale"
	"github.com/guided-traffic/cogpit/internal/mcp"
	"github.com/guided-traffic/cogpit/internal/ui/fyneui"
	"github.com/guided-traffic/cogpit/internal/update"
)

func main() {
	// Not deferred: on a panic of the main goroutine the defers run before the runtime writes the
	// crash, and done would remove the report (crash.go).
	previousCrash, done := captureCrashes(crashReportDir())
	code := run(previousCrash)
	done()
	os.Exit(code)
}

func run(previousCrash string) int {
	var (
		kubeconfig  = flag.String("kubeconfig", "", "kubeconfig files, separated by '"+string(os.PathListSeparator)+"', offered as sources of their own for this run; default: the files $KUBECONFIG lists. ~/.kube is always a sync folder")
		contextName = flag.String("context", "", "connect to this context at start; without it Cogpit starts on the welcome page")
		demo        = registerDemoFlags() // a development build's --demo, --demo-pods, --demo-events (demo.go); a release has none (D-67)
		mcpAddr     = flag.String("mcp-addr", "127.0.0.1:0", "MCP listen address; loopback only")
		noMCP       = flag.Bool("no-mcp", false, "do not start the MCP server")
		perf        = flag.Duration("perf", 0, "measure main-loop latency while auto-scrolling the table for this long, print the result as JSON and quit")
		perfMode    = flag.String("perf-mode", "", "with --perf (or alone for gate): scroll, or gate for start-up, scrolling, every view and typing into 5 MB of YAML in one run")
		cpuProfile  = flag.String("cpuprofile", "", "write a CPU profile of the first --cpuprofile-for to this file")
		cpuFor      = flag.Duration("cpuprofile-for", 5*time.Second, "how long --cpuprofile records")
		themeName   = flag.String("theme", "", "colour theme for this run: dark, light or system; default: the Preferences' theme (system until set)")
		verbose     = flag.Bool("v", false, "debug logging")
	)
	flag.Parse()

	if *cpuProfile != "" {
		stop, err := startCPUProfile(*cpuProfile, *cpuFor)
		if err != nil {
			fmt.Fprintln(os.Stderr, "cogpit:", err)
			return 1
		}
		defer stop()
	}

	// Helm values never reach the log; Fyne's thread-check warnings, which come through the
	// standard log package that slog.SetDefault routes here, reach the window too (D-103).
	logger := slog.New(fyneui.WatchThreadChecks(helm.RedactSDKLog(newLogger(*verbose).Handler())))
	slog.SetDefault(logger)
	klog.SetSlogLogger(logger)
	logger.Info("cogpit started", "version", buildinfo.Read().String(), "fyneThreadCheck", buildinfo.ThreadChecksText())
	// cogpit:// links (D-47): the handler must be in place before the app finishes launching, when
	// macOS delivers the link that started it; the links wait in the queue for the window.
	deepLinkQueue := installDeepLinks(logger)
	// client-go's informer goroutines re-panic after logging by default, which would end the app
	// because one watch failed (D-11: crash-tolerant UI). Log instead; the cache reports the stopped
	// informer as an error state.
	utilruntime.ReallyCrash = false
	utilruntime.PanicHandlers = append(utilruntime.PanicHandlers, func(_ context.Context, r any) {
		logger.Error("recovered panic in a client-go goroutine", "panic", fmt.Sprint(r))
	})

	// A GUI app started from Finder lacks the shell's PATH; exec credential plugins and kubectl
	// live there (gap H-6).
	cluster.EnsureToolPath()

	audit, auditPath, closeAudit := openAuditLog(logger)
	defer closeAudit()

	clusterSettings := openClusterSettings(logger) // the session's, and the connector's proxies
	sources := openKubeconfigSources(logger, cluster.NamedKubeconfigFiles(filepath.SplitList(*kubeconfig)))
	// macOS's grants of the files and folders the user picked in the system's panels, before
	// anything reads a source (D-72, fileaccess.go).
	bookmarks := restoreFileAccess(logger, sources)
	connector := &engine.ClusterConnector{
		KubeconfigPaths: filepath.SplitList(*kubeconfig),
		Logger:          logger,
		Sources:         sources,
		MetricsSettings: openMetricsSettings(logger),
		ClusterSettings: clusterSettings,
		// Discovery answers kept for the next connection, shared with cogpit-cli (D-65).
		DiscoveryCacheDir: discoveryCacheDir(),
	}
	demo.offer(connector) // the demo cluster, in a development build only
	// Every time the window writes is in the computer's region format (D-31), read once here.
	timeFormat := locale.System()
	logger.Debug("time format", "source", timeFormat.Source, "date", timeFormat.Layout.Date, "time", timeFormat.Layout.Time)
	s := engine.New(engine.Options{Connector: connector, Audit: audit, AuditPath: auditPath, ConnectLogDir: connectLogDir(),
		Logger: logger, Drains: openDrainJournal(logger),
		NodeShells: openNodeShellJournal(logger), Helm: openHelmJournal(logger), Hotbar: openHotbar(logger), HelmRepos: openHelmRepos(logger),
		Preferences: openPreferences(logger), ClusterSettings: clusterSettings, Kubectl: openKubectl(logger),
		Drafts: openDrafts(logger), Workspace: openWorkspace(logger), TimeLayout: timeFormat.Layout,
		LoginItem: newLoginItem()}) // macOS: Preferences › App › Start at login (loginitem_darwin.go); elsewhere none
	defer s.Close()
	defer s.WatchKubeconfigs(0)() // sync folders: files that appear or leave are picked up
	if err := commands.RegisterAll(s.Registry()); err != nil {
		fmt.Fprintln(os.Stderr, "cogpit:", err)
		return 1
	}
	installChat(s, logger) // the built-in agent's chat (chat.go, D-10)
	// The macOS app's updater, Sparkle 2 from the app bundle (D-4, internal/update): none for a
	// build without the update key or outside an installed bundle, and the engine says why (Help ›
	// Check for Updates…). It starts once the window's event loop runs.
	s.SetUpdater(update.New())

	opts := fyneui.Options{AutoConnect: *contextName, Theme: fyneui.ThemeMode(*themeName),
		DeepLinks: deepLinkQueue, Bookmarks: bookmarks, WindowPlace: windowPlacePath()}
	if previousCrash != "" {
		at := time.Now()
		if fi, err := os.Stat(previousCrash); err == nil {
			at = fi.ModTime()
		}
		logger.Warn("the last run of Cogpit crashed", "report", previousCrash)
		opts.StartWarning = crashNotice(previousCrash, at, s.TimeFormat())
	}
	demo.autoConnect(&opts)
	// Preferences › App › Restore the last session (D-69 item 8): with it on, the clusters connected
	// at the last quit connect again, the one shown then first unless --context names the one to
	// show; off (the default), nothing connects by itself.
	s.RestoreLastSession(opts.AutoConnect)
	if !*noMCP {
		srv, err := mcp.New(s, mcp.Options{Addr: *mcpAddr, Version: buildinfo.Version(), Logger: logger})
		if err != nil {
			logger.Warn("MCP server not started", "err", err)
		} else {
			// The window starts the server, unless the user switched it off in an earlier run, and
			// switches it from the status bar; the choice is kept where cogpit-cli mcp reads it too
			// (D-49). Quitting closes it for good. --no-mcp wins over the stored choice for this
			// run: no server, no item, the stored choice left as it is.
			defer func() { _ = srv.Close() }()
			ctl := &mcpControl{srv: srv, session: s, logger: logger, out: os.Stdout, cli: bundledCLI()}
			if dirs, err := app.UserDirs(); err == nil {
				ctl.config = dirs.Config
			}
			opts.MCP = ctl
		}
	}
	if *perf > 0 || *perfMode != "" {
		opts.PerfDuration = *perf
		opts.PerfMode = *perfMode
		opts.PerfReport = func(ps fyneui.PerfStats) {
			b, _ := json.Marshal(ps)
			fmt.Println(string(b))
		}
	}
	if err := fyneui.Run(s, opts); err != nil {
		fmt.Fprintln(os.Stderr, "cogpit:", err)
		return 1
	}
	return 0
}

// discoveryCacheDir is <user cache dir>/Cogpit/discovery (app.Dirs.DiscoveryCacheDir); empty, and
// discovery in memory only, without a cache directory.
func discoveryCacheDir() string {
	dirs, err := app.UserDirs()
	if err != nil {
		return ""
	}
	return dirs.DiscoveryCacheDir()
}

// windowPlacePath is <user config dir>/Cogpit/window.json, the window's place and size (D-95);
// empty, and nothing kept, without a configuration directory.
func windowPlacePath() string {
	dirs, err := app.UserDirs()
	if err != nil {
		return ""
	}
	return filepath.Join(dirs.Config, fyneui.WindowPlaceFile)
}

// connectLogDir is <user config dir>/Cogpit/connection-logs (app.Dirs.ConnectLogDir, D-66); empty,
// and the connection log in memory only, without a configuration directory.
func connectLogDir() string {
	dirs, err := app.UserDirs()
	if err != nil {
		return ""
	}
	return dirs.ConnectLogDir()
}

// newLogger logs to stderr and to <user cache dir>/Cogpit/cogpit.log, so a Cogpit.app started
// from Finder keeps its log too.
func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	var w io.Writer = os.Stderr
	if dir, err := os.UserCacheDir(); err == nil {
		dir = filepath.Join(dir, "Cogpit")
		if err := os.MkdirAll(dir, 0o700); err == nil {
			if f, err := os.OpenFile(filepath.Join(dir, "cogpit.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
				w = io.MultiWriter(os.Stderr, f)
			}
		}
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

// openAuditLog opens <user config dir>/Cogpit/audit.log, owner-only, for agent-initiated
// commands (D-2; app.Dirs.OpenAuditLog, shared with cogpit-cli), and returns its path, which the
// Audit Log reads back.
func openAuditLog(logger *slog.Logger) (io.Writer, string, func()) {
	dirs, err := app.UserDirs()
	if err != nil {
		logger.Warn("audit log disabled", "err", err)
		return nil, "", func() {}
	}
	f, err := dirs.OpenAuditLog()
	if err != nil {
		logger.Warn("audit log disabled", "err", err)
		return nil, "", func() {}
	}
	return f, dirs.AuditLogPath(), func() { _ = f.Close() }
}

// openDrainJournal keeps the drains Cogpit starts in <user config dir>/Cogpit/drains.json,
// owner-only, so that a drain a crash interrupts is offered again (D-11). Without a configuration
// directory there is no journal, and node.drain is refused rather than run without one.
func openDrainJournal(logger *slog.Logger) *engine.Drains {
	dir, err := os.UserConfigDir()
	if err != nil {
		logger.Warn("drain journal disabled; node drains are refused", "err", err)
		return nil
	}
	return engine.NewDrains(engine.NewFileDrainJournal(filepath.Join(dir, "Cogpit")))
}

// openNodeShellJournal keeps the node shell pods Cogpit creates in
// <user config dir>/Cogpit/nodeshells.json, owner-only, so that one a crash leaves is deleted at
// the next connect to its context (H-71). Without a configuration directory the engine keeps them
// in memory, and such a pod lives until its deadline.
func openNodeShellJournal(logger *slog.Logger) engine.NodeShellJournal {
	dir, err := os.UserConfigDir()
	if err != nil {
		logger.Warn("node shell journal kept in memory; a pod a crash leaves lives until its deadline", "err", err)
		return nil
	}
	return engine.NewFileNodeShellJournal(filepath.Join(dir, "Cogpit"))
}

// openHelmJournal keeps the Helm rollbacks and uninstalls Cogpit starts in
// <user config dir>/Cogpit/helm.json, owner-only, so that one a crash interrupts is offered again
// (D-11). Without a configuration directory there is no journal, and they are refused.
func openHelmJournal(logger *slog.Logger) *engine.HelmOps {
	dir, err := os.UserConfigDir()
	if err != nil {
		logger.Warn("Helm journal disabled; release rollbacks and uninstalls are refused", "err", err)
		return nil
	}
	return engine.NewHelmOps(engine.NewFileHelmJournal(filepath.Join(dir, "Cogpit")))
}

// openDrafts keeps the unsaved text of Edit and Create resource tabs in
// <user config dir>/Cogpit/drafts/, owner-only, so that a crash or a quit does not lose it (D-11);
// never a Secret's text or Helm values. Without a configuration directory there are no drafts.
func openDrafts(logger *slog.Logger) *engine.Drafts {
	dir, err := os.UserConfigDir()
	if err != nil {
		logger.Warn("drafts of unsaved editor text disabled", "err", err)
		return nil
	}
	return engine.NewDrafts(filepath.Join(dir, "Cogpit"))
}

// openWorkspace keeps the tab bar's tabs, the clusters connected and the tables' column layout in
// <user config dir>/Cogpit/workspace.json, owner-only (D-41): the layout comes back at every start,
// the tabs and the connections only while Preferences › App › Restore the last session is on
// (D-69 item 8, RestoreLastSession). Without a configuration directory they last for the run.
func openWorkspace(logger *slog.Logger) *engine.Workspace {
	dir, err := os.UserConfigDir()
	if err != nil {
		logger.Warn("the tabs and the column layout are not kept after this run", "err", err)
		return nil
	}
	return engine.NewWorkspace(filepath.Join(dir, "Cogpit"))
}

// openHotbar keeps the hotbar's pinned clusters in <user config dir>/Cogpit/hotbar.json (D-83: one list),
// owner-only (G13). Without a configuration directory the hotbar lives in memory for the run.
func openHotbar(logger *slog.Logger) *engine.Hotbar {
	dir, err := os.UserConfigDir()
	if err != nil {
		logger.Warn("the hotbar is not kept after this run", "err", err)
		return engine.NewHotbar("")
	}
	return engine.NewHotbar(filepath.Join(dir, "Cogpit"))
}

// openPreferences keeps Cogpit's preferences (theme, time zone, editor, terminal, tables) in
// <user config dir>/Cogpit/preferences.json, owner-only (G13). Without a configuration directory
// they live in memory for the run.
func openPreferences(logger *slog.Logger) *engine.PreferencesStore {
	dir, err := os.UserConfigDir()
	if err != nil {
		logger.Warn("preferences are not kept after this run", "err", err)
		return engine.NewPreferencesStore("")
	}
	return engine.NewPreferencesStore(filepath.Join(dir, "Cogpit"))
}

// openKubectl checks the kubectl of the terminals against their clusters and keeps the releases the
// user downloads in <user config dir>/Cogpit/kubectl/<version>/kubectl, owner-only, unless the
// Preferences name another directory (D-3). Without a configuration directory only the
// Preferences' directory can hold downloads.
func openKubectl(logger *slog.Logger) *engine.Kubectl {
	dir, err := os.UserConfigDir()
	if err != nil {
		logger.Warn("kubectl downloads need a directory for binaries in the Preferences", "err", err)
		return engine.NewKubectl("", engine.KubectlHooks{})
	}
	return engine.NewKubectl(filepath.Join(dir, "Cogpit", engine.KubectlDirName), engine.KubectlHooks{})
}

// openClusterSettings keeps each cluster's display name, icon and terminal settings in
// <user config dir>/Cogpit/cluster-settings.json and its uploaded icons in cluster-icons/, owner-only,
// never in the kubeconfig (G13). Without a configuration directory they live in memory for the run.
func openClusterSettings(logger *slog.Logger) *engine.ClusterSettingsStore {
	dir, err := os.UserConfigDir()
	if err != nil {
		logger.Warn("cluster settings are not kept after this run", "err", err)
		return engine.NewClusterSettingsStore("")
	}
	return engine.NewClusterSettingsStore(filepath.Join(dir, "Cogpit"))
}

// openMetricsSettings keeps the Prometheus each cluster's metrics come from, as the user set it,
// in <user config dir>/Cogpit/metrics.json, owner-only (never in the kubeconfig). Without a
// configuration directory a setting lasts for the connection.
func openMetricsSettings(logger *slog.Logger) *engine.MetricsSettings {
	dir, err := os.UserConfigDir()
	if err != nil {
		logger.Warn("metrics settings are not kept", "err", err)
		return nil
	}
	m, err := engine.NewMetricsSettings(filepath.Join(dir, "Cogpit"))
	if err != nil {
		logger.Warn("metrics settings could not be read; starting without", "err", err)
	}
	return m
}

// openHelmRepos reads the Helm chart repositories of Helm's own repositories.yaml, resolved here
// and only here as the helm CLI resolves it ($HELM_REPOSITORY_CONFIG, else $HELM_CONFIG_HOME, else
// Helm's configuration folder; helm.RepositoryConfigPath), and keeps their indexes in
// <user cache dir>/Cogpit/helm/indexes, owner-only (D-29, G11). Cogpit writes nothing into Helm's
// file or folders. Without a cache directory the indexes go under the configuration directory;
// without either there are no repositories. The user's allowances to send a repository's user and
// password over plain http live in <user config dir>/Cogpit/helm-http-approvals.json, owner-only
// (credentials H-40).
func openHelmRepos(logger *slog.Logger) *engine.HelmRepos {
	cache, err := os.UserCacheDir()
	if err != nil {
		dir, cerr := os.UserConfigDir()
		if cerr != nil {
			logger.Warn("Helm repositories disabled; charts cannot be installed", "err", err)
			return nil
		}
		cache = filepath.Join(dir, "Cogpit", "cache")
	} else {
		cache = filepath.Join(cache, "Cogpit")
	}
	file := helm.RepositoryConfigPath()
	logger.Info("Helm repositories are read from Helm's repositories file", "file", file)
	var opts []engine.HelmReposOption
	if dirs, err := app.UserDirs(); err == nil {
		// The user's allowances to send a repository's credentials over plain http (H-40).
		opts = append(opts, engine.WithHTTPApprovals(dirs.HelmHTTPApprovalsPath()))
	} else {
		logger.Warn("allowances of Helm credentials over http last for this session", "err", err)
	}
	return engine.NewHelmRepos(file, cache, opts...)
}

// openKubeconfigSources keeps the kubeconfig files and sync folders the user
// adds in <user config dir>/Cogpit (kubeconfigs.json, exec-approvals.json), each owner-only.
// Without a configuration directory there are none, and the kubeconfig files are merged as kubectl
// merges them.
//
// Every context comes from a source file read on its own; nothing is merged (D-28). The user's own
// ~/.kube (%USERPROFILE%\.kube on Windows), config included, is a sync folder from the start, as
// in Lens, removable like any other. The files named for this run (named: --kubeconfig, else
// $KUBECONFIG) are sources of their own, removable for the session. Cogpit reads these files,
// never writes them, and a credential plugin of any of them needs the user's approval
// (trust-boundaries H-1). Without a home directory there is no default folder. cogpit-cli builds
// the same sources (app.Dirs.KubeconfigSources).
func openKubeconfigSources(logger *slog.Logger, named []string) *engine.KubeconfigSources {
	dirs, err := app.UserDirs()
	if err != nil {
		logger.Warn("kubeconfig sources disabled; the kubeconfig files are merged as kubectl does", "err", err)
		return nil
	}
	return dirs.KubeconfigSources(named)
}

// startCPUProfile records a CPU profile for d (or until stop is called) into path; for the gate
// measurements of the start-up.
func startCPUProfile(path string, d time.Duration) (stop func(), err error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	var once sync.Once
	stop = func() {
		once.Do(func() {
			pprof.StopCPUProfile()
			_ = f.Close()
		})
	}
	time.AfterFunc(d, stop)
	return stop, nil
}
