//go:build !windows

package terminal

import (
	"path/filepath"
	"testing"
	"time"
)

// The shell gets the Preferences' arguments after its name and starts in the cluster's working
// directory (Cluster settings › Terminal).
func TestStartLocalPassesArgsAndDir(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	sess, err := StartLocal(LocalOptions{
		KubeconfigPaths: []string{writeUserKubeconfig(t)}, Context: "ctx-a",
		Shell: "/bin/sh", Args: []string{"-c", `pwd -P > out.txt; printf '%s\n' "$KUBECONFIG" >> out.txt`}, Dir: work,
		Env: []string{"HOME=" + home, "ENV=", "ZDOTDIR=" + home},
	})
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, sess)
	waitClosed(t, sess.Done(), 10*time.Second, "the shell running -c")
	out := readTrimmed(t, filepath.Join(work, "out.txt"))
	want, _ := filepath.EvalSymlinks(work)
	if len(out) < len(want) || out[:len(want)] != want {
		t.Errorf("the shell ran in %q, want %q", out, want)
	}
}
