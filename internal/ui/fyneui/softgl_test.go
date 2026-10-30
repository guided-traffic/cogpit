package fyneui

import (
	"path/filepath"
	"strings"
	"testing"
)

// Cogpit names llvmpipe only when the opengl32.dll it loaded lies beside its executable and the
// user named no driver; the folders compare without case.
func TestSoftGLOnlyForMesaBesideCogpit(t *testing.T) {
	dir := filepath.Join("C:", "Users", "someone", "AppData", "Local", "Programs", "Cogpit")
	exe := filepath.Join(dir, "cogpit.exe")
	beside := filepath.Join(dir, "opengl32.dll")
	for _, c := range []struct {
		name, dll, exe, driver string
		mesa                   bool
		set                    string
	}{
		{"Mesa beside, no driver named", beside, exe, "", true, softGLDriver},
		{"the folder in another case", filepath.Join(strings.ToUpper(dir), "OPENGL32.DLL"), exe, "", true, softGLDriver},
		{"the user's driver stays", beside, exe, "d3d12", true, ""},
		{"the system's OpenGL", filepath.Join("C:", "Windows", "System32", "opengl32.dll"), exe, "", false, ""},
		{"in a folder below Cogpit's", filepath.Join(dir, "mesa", "opengl32.dll"), exe, "", false, ""},
		{"no opengl32.dll loaded", "", exe, "", false, ""},
		{"no executable", beside, "", "", false, ""},
	} {
		if mesa, set := softGL(c.dll, c.exe, c.driver); mesa != c.mesa || set != c.set {
			t.Errorf("%s: softGL = %v, %q, want %v, %q", c.name, mesa, set, c.mesa, c.set)
		}
	}
}
