package cluster

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// noteUnreadable is the note of a file that cannot be opened, statted or read.
const noteUnreadable = "unreadable"

// Bounds of the content hashing of files a kubeconfig names (R-5, D-26).
const (
	// MaxBoundProgramSize is the largest program inside a kubeconfig's folder Cogpit hashes. A
	// credential plugin is a script or a small binary; a larger file is bound by its size and
	// modification time instead (FileFacts.ProgramNote).
	MaxBoundProgramSize = 32 << 20
	// MaxBoundCASize is the same for a CA file.
	MaxBoundCASize = 1 << 20
)

// BindPluginFiles looks at the files the login p names and records what its approval binds beyond
// their paths (kubeconfig.FileFacts): the SHA-256 of the program and of the CA file when they
// resolve to a file inside the folder of p.File, the symlink-resolved paths, and whether the token
// file lies inside that folder. It reads files (at most MaxBoundProgramSize and MaxBoundCASize
// bytes, regular files only) and runs nothing. It returns p, or nil when the login then needs no
// approval (a token file inside the folder and no program).
//
// It runs when a file is described for the user and again at connect time, right before the
// policy decides, so a file swapped in between is caught.
func BindPluginFiles(p *kubeconfig.ExecPlugin) *kubeconfig.ExecPlugin {
	if p == nil {
		return nil
	}
	folder := realFolder(p.File)
	var f kubeconfig.FileFacts

	if p.Command != "" {
		path := p.Command
		if !strings.ContainsRune(path, filepath.Separator) {
			path = LookPathNow(path) // client-go looks a bare name up on PATH
		} else if runtime.GOOS == "windows" {
			// os/exec (client-go's exec.Command) runs a path without one of PATHEXT's extensions
			// (.com, .exe, .bat, .cmd, …) as the first of the path with each that exists: plug runs
			// plug.exe there, which is the file to bind, not a plug beside it.
			if found := LookPathNow(path); found != "" {
				path = found
			}
		}
		f.ProgramResolved, f.ProgramSHA256, f.ProgramNote = bindFile(path, folder, MaxBoundProgramSize)
	}
	if ca := p.CAFilePath(); ca != "" {
		f.CAFileResolved, f.CAFileSHA256, f.CAFileNote = bindFile(absAgainst(ca, p.File), folder, MaxBoundCASize)
	}
	if p.TokenFile != "" {
		tf := absAgainst(p.TokenFile, p.File)
		resolved := resolveOrClean(tf)
		if lexical := lexicalPath(tf); resolved != lexical && folder != "" && within(folder, lexical) {
			f.TokenFileResolved = resolved // a link inside the folder: repointing it asks again
		}
		f.TokenInFolder = folder != "" && within(folder, resolved)
	}
	p.SetFiles(f)
	if !p.NeedsApproval() {
		return nil
	}
	return p
}

// bindFile returns what an approval binds about the file at path: when the path itself lies inside
// folder, its symlink-resolved path if that differs (the link is the folder writer's to repoint);
// and, when the resolved file lies inside folder, the hex SHA-256 of its content or a note on why
// it has none. A file resolved outside the folder is bound by its path alone: a tool update on
// PATH (a Homebrew link, kubelogin) must not ask again.
func bindFile(path, folder string, limit int64) (resolved, digest, note string) {
	if path == "" || folder == "" {
		return "", "", ""
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", "" // missing: connecting fails by itself, and the path is bound
	}
	if lexical := lexicalPath(path); target != lexical && within(folder, lexical) {
		resolved = target
	}
	if within(folder, target) {
		digest, note = hashRegular(target, limit)
	}
	return resolved, digest, note
}

// lexicalPath returns path with the symlinks of its directories resolved and its last element kept
// as written: where a link in a folder sits, not where it points.
func lexicalPath(path string) string {
	return filepath.Join(resolveOrClean(filepath.Dir(path)), filepath.Base(path))
}

// hashRegular returns the hex SHA-256 of the regular file at path, or a note when it is not a
// regular file, is unreadable, or is larger than limit. The note for a file that is too large
// carries its size and modification time, which the approval binds in place of the content.
func hashRegular(path string, limit int64) (digest, note string) {
	f, err := os.Open(path)
	if err != nil {
		return "", noteUnreadable
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat() // of the opened file: no swap between the check and the read
	if err != nil {
		return "", noteUnreadable
	}
	if !fi.Mode().IsRegular() {
		return "", "not a regular file"
	}
	if fi.Size() > limit {
		return "", fmt.Sprintf("larger than %d bytes (%d bytes, modified %s)", limit, fi.Size(), fi.ModTime().UTC().Format(time.RFC3339Nano))
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, limit+1))
	if err != nil {
		return "", noteUnreadable
	}
	if n > limit {
		return "", fmt.Sprintf("grew past %d bytes while being read", limit)
	}
	return hex.EncodeToString(h.Sum(nil)), ""
}

// realFolder returns the symlink-resolved directory of file; empty when file is empty.
func realFolder(file string) string {
	if file == "" {
		return ""
	}
	return resolveOrClean(filepath.Dir(file))
}

// resolveOrClean resolves symlinks in path; a path that does not exist is returned cleaned, with
// its directory resolved when that exists.
func resolveOrClean(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}
	if r, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		return filepath.Join(r, filepath.Base(path))
	}
	return filepath.Clean(path)
}

// absAgainst makes path absolute against the directory of file.
func absAgainst(path, file string) string {
	if filepath.IsAbs(path) || file == "" {
		return filepath.Clean(path)
	}
	return filepath.Join(filepath.Dir(file), path)
}

// within reports whether path lies inside dir, at any depth. Both are clean and absolute.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
