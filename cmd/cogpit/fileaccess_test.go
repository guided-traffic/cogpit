package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/ui/nativedialog"
)

// At start a kubeconfig source's bookmark stays while the path is a source, a folder's and a CA
// file's stay as they are; without sources (no configuration directory) every bookmark stays.
func TestKeptSource(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "team")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	sources := engine.NewKubeconfigSources(filepath.Join(dir, "Cogpit"))
	if _, err := sources.AddFolder(folder); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	keep := keptSource(sources, logger)
	for _, tc := range []struct {
		e    nativedialog.Entry
		want bool
	}{
		{nativedialog.Entry{Use: nativedialog.UseKubeconfig, Path: folder}, true},
		{nativedialog.Entry{Use: nativedialog.UseKubeconfig, Path: folder + "/"}, true},
		{nativedialog.Entry{Use: nativedialog.UseKubeconfig, Path: filepath.Join(dir, "removed.yaml")}, false},
		{nativedialog.Entry{Use: nativedialog.UseFolder, Path: filepath.Join(dir, "work")}, true},
		{nativedialog.Entry{Use: nativedialog.UseCAFile, Path: filepath.Join(dir, "ca.pem")}, true},
	} {
		if got := keep(tc.e); got != tc.want {
			t.Errorf("keep(%+v) = %v, want %v", tc.e, got, tc.want)
		}
	}
	if keptSource(nil, logger) != nil {
		t.Error("without sources a bookmark was judged")
	}
}

// Removing a kubeconfig source drops its bookmark at once; the other sources' bookmarks and those
// of other uses stay.
func TestRemovedSourceDropsItsBookmark(t *testing.T) {
	dir := t.TempDir()
	team, other := filepath.Join(dir, "team"), filepath.Join(dir, "other")
	for _, f := range []string{team, other} {
		if err := os.Mkdir(f, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	sources := engine.NewKubeconfigSources(filepath.Join(dir, "Cogpit"))
	src, err := sources.AddFolder(team)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sources.AddFolder(other); err != nil {
		t.Fatal(err)
	}
	store, err := nativedialog.OpenStore(filepath.Join(dir, "Cogpit", nativedialog.BookmarksFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []nativedialog.Entry{
		{Use: nativedialog.UseKubeconfig, Path: team}, {Use: nativedialog.UseKubeconfig, Path: other},
		{Use: nativedialog.UseFolder, Path: team},
	} {
		if err := store.Keep(e.Use, e.Path, nativedialog.ReadOnly, []byte("b")); err != nil {
			t.Fatal(err)
		}
	}
	forgetRemovedSources(store, sources, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := sources.Remove(src.ID); err != nil {
		t.Fatal(err)
	}
	got := store.Entries()
	if len(got) != 2 || got[0].Path != other || got[1].Use != nativedialog.UseFolder {
		t.Errorf("bookmarks after the remove: %+v, want other's and the folder use's", got)
	}
}
