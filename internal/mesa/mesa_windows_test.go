//go:build windows

package mesa

import (
	"path/filepath"
	"strings"
	"testing"
)

// go-gl links opengl32.dll, but this package does not: the test binary has kernel32.dll loaded from
// the start, so loadedModule finds it by a path that ends in its name, and finds nothing for a
// module the process has not loaded.
func TestLoadedModule(t *testing.T) {
	p := loadedModule("kernel32.dll")
	if !strings.EqualFold(filepath.Base(p), "kernel32.dll") {
		t.Fatalf("loadedModule(kernel32.dll) = %q", p)
	}
	if p := loadedModule("cogpit-no-such-module.dll"); p != "" {
		t.Errorf("a module the process has not loaded: %q", p)
	}
}

// Every user32 function the boxes call is there.
func TestUser32HasTheBoxesFunctions(t *testing.T) {
	for _, p := range []interface {
		Find() error
	}{procFindWindowW, procSetDlgItemTextW, procGetDlgItem, procEnableWindow, procPostMessageW,
		procEnumThreadWins, procIsWindowVisible, procShowWindow} {
		if err := p.Find(); err != nil {
			t.Error(err)
		}
	}
}
