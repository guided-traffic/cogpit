//go:build windows

package fyneui

import (
	"path/filepath"
	"strings"
	"testing"
)

// go-gl links opengl32.dll, so this test binary has it loaded from the start: loadedModule finds
// it by a path that ends in its name (on a runner, System32's), and finds nothing for a module the
// process has not loaded.
func TestLoadedModuleFindsOpenGL(t *testing.T) {
	p := loadedModule("opengl32.dll")
	if !strings.EqualFold(filepath.Base(p), "opengl32.dll") {
		t.Fatalf("loadedModule(opengl32.dll) = %q", p)
	}
	t.Logf("opengl32.dll: %s", p)
	if p := loadedModule("cogpit-no-such-module.dll"); p != "" {
		t.Errorf("a module the process has not loaded: %q", p)
	}
}
