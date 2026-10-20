package fyneui

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	"github.com/guided-traffic/cogpit/internal/buildinfo"
)

// Fyne's thread check (D-103). A build with the check on (FYNE_THREAD_CHECKS=on, the default of
// development builds) has Fyne test on every canvas refresh that it runs on the main goroutine; a
// call from another goroutine is moved there and logged through the standard log package, which
// cmd/cogpit's slog.SetDefault routes into the app log: "*** Error in Fyne call thread, …", two
// more lines, then "  From: <file>:<line>", the first frame outside Fyne. A release turns the
// check off (FYNE_THREAD_CHECKS=off, appMetadata): such a call would race there, so a development
// build shows each place in a toast as well, once per place, to be fixed before it ships.

// strayCalls collects the places Fyne reported; the shell that runs shows them.
var strayCalls = &strayCallLog{seen: map[string]bool{}}

// strayCallLog pairs Fyne's warning with its "From:" line and hands each new place to notify.
type strayCallLog struct {
	mu      sync.Mutex
	pending bool            // a warning came; its "From:" line has not
	seen    map[string]bool // the places reported so far in this process
	notify  func(site string)
}

// note reads one line of the app log.
func (l *strayCallLog) note(msg string) {
	l.mu.Lock()
	switch {
	case strings.HasPrefix(msg, "*** Error in Fyne call thread"):
		l.pending = true
		l.mu.Unlock()
		return
	case !l.pending || !strings.HasPrefix(strings.TrimSpace(msg), "From: "):
		l.mu.Unlock()
		return
	}
	l.pending = false
	site := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(msg), "From: "))
	if l.seen[site] {
		l.mu.Unlock()
		return
	}
	l.seen[site] = true
	notify := l.notify
	l.mu.Unlock()
	if notify != nil {
		notify(site)
	}
}

// watch makes notify receive each new place from now on; one reported before (before the window
// started) is in the app log only. The returned func stops it.
func (l *strayCallLog) watch(notify func(site string)) (stop func()) {
	l.mu.Lock()
	l.notify = notify
	l.mu.Unlock()
	return func() {
		l.mu.Lock()
		l.notify = nil
		l.mu.Unlock()
	}
}

// WatchThreadChecks wraps the app log's handler so that Fyne's thread-check warnings reach the
// window too (D-103); every record still goes to h unchanged. A build without the check logs none.
func WatchThreadChecks(h slog.Handler) slog.Handler {
	if !buildinfo.FyneThreadChecks {
		return h
	}
	return threadCheckHandler{Handler: h, log: strayCalls}
}

type threadCheckHandler struct {
	slog.Handler
	log *strayCallLog
}

func (h threadCheckHandler) Handle(ctx context.Context, r slog.Record) error {
	h.log.note(r.Message)
	return h.Handler.Handle(ctx, r)
}

func (h threadCheckHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return threadCheckHandler{Handler: h.Handler.WithAttrs(attrs), log: h.log}
}

func (h threadCheckHandler) WithGroup(name string) slog.Handler {
	return threadCheckHandler{Handler: h.Handler.WithGroup(name), log: h.log}
}

// watchStrayCalls shows, in a development build with the check on, each place Fyne reported as a
// warning toast; a release (with or without the check) keeps them in the app log only.
func (sh *shell) watchStrayCalls() (stop func()) {
	if !buildinfo.FyneThreadChecks || buildinfo.Read().Release {
		return func() {}
	}
	return strayCalls.watch(func(site string) {
		sh.do(func() { sh.toast("warning", strayCallMessage(site)) })
	})
}

// strayCallMessage names the place by its package directory and file: enough to find it, short
// enough for a toast.
func strayCallMessage(site string) string {
	short := site
	if dir, file := filepath.Split(site); dir != "" {
		short = filepath.Join(filepath.Base(dir), file)
	}
	return "Development build: a Fyne call ran off the main goroutine at " + short +
		". Fyne moved it there; a release build, with the thread check off, would race (D-103). The app log has the details."
}
