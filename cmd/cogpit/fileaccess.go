package main

import (
	"log/slog"
	"path/filepath"

	"github.com/guided-traffic/cogpit/internal/app"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/ui/nativedialog"
)

// restoreFileAccess opens the bookmarks of what the user picked in the system's open panel and
// Cogpit reads again (D-72, internal/ui/nativedialog: <user config dir>/Cogpit/bookmarks.json,
// owner-only), resolves them and starts accessing their items, before anything reads a
// kubeconfig source: macOS granted them when the user picked them, and the bookmark is how an app
// carries that grant into a later run. A kubeconfig source's bookmark goes with the source: when
// the user removes it, and at start for a path that is no source; the others are bounded by the
// store. Without a configuration directory the window keeps
// the bookmarks of this run only. The log names counts, never paths.
func restoreFileAccess(logger *slog.Logger, sources *engine.KubeconfigSources) *nativedialog.Store {
	path := ""
	if dirs, err := app.UserDirs(); err == nil {
		path = filepath.Join(dirs.Config, nativedialog.BookmarksFile)
	} else {
		logger.Warn("the access granted to picked files lasts for this session", "err", err)
	}
	store, err := nativedialog.OpenStore(path)
	if err != nil {
		logger.Warn("the bookmarks of picked files could not be read; the next pick replaces them", "err", err)
	}
	rep, err := store.Restore(keptSource(sources, logger))
	if err != nil {
		logger.Warn("the bookmarks of picked files could not be written", "err", err)
	}
	logger.Debug("access to picked files restored", "restored", rep.Restored, "accessing", rep.Accessing,
		"refreshed", rep.Refreshed, "dropped", rep.Dropped, "failed", rep.Failed)
	forgetRemovedSources(store, sources, logger)
	return store
}

// forgetRemovedSources drops a kubeconfig source's bookmark when the user removes the source, as
// the next start would: the grant does not outlive the source. The access started for it lasts
// until Cogpit quits.
func forgetRemovedSources(store *nativedialog.Store, sources *engine.KubeconfigSources, logger *slog.Logger) {
	if sources == nil {
		return
	}
	sources.OnRemove(func(engine.KubeconfigSource) {
		n, err := store.Drop(keptSource(sources, logger))
		if err != nil {
			logger.Warn("the bookmarks of picked files could not be written", "err", err)
		}
		logger.Debug("bookmarks of removed kubeconfig sources dropped", "dropped", n)
	})
}

// keptSource keeps a kubeconfig bookmark while its path is a source; the other uses' bookmarks
// stay. When the sources cannot be listed every bookmark stays for this run.
func keptSource(sources *engine.KubeconfigSources, logger *slog.Logger) func(nativedialog.Entry) bool {
	if sources == nil {
		return nil
	}
	list, err := sources.List()
	if err != nil {
		logger.Warn("kubeconfig sources not listed; every bookmark is kept", "err", err)
		return nil
	}
	paths := make(map[string]bool, len(list))
	for _, s := range list {
		paths[filepath.Clean(s.Path)] = true
	}
	return func(e nativedialog.Entry) bool {
		return e.Use != nativedialog.UseKubeconfig || paths[filepath.Clean(e.Path)]
	}
}
