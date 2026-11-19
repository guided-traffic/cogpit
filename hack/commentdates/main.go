// Command commentdates fails when a Go comment carries a calendar date (YYYY-MM-DD): comments
// state the current code and its reasons, and git holds when a line was written
// (docs/developer/conventions.md#documentation). It reads every .go file under the given
// directories (default: the working directory), skipping testdata, vendor and dot directories,
// and prints each date as file:line:column with its comment line.
//
// A date that is data, not history, passes: one in quotes on its comment line (an example value,
// a protocol version), one followed by a time of day (a timestamp), and Go's reference date
// 2006-01-02 (a layout). String literals are never read.
//
//	go run ./hack/commentdates          # make lint runs it
package main

import (
	"fmt"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// date is a calendar date; \b leaves out the day of an RFC 3339 timestamp, which a T follows.
var date = regexp.MustCompile(`\b20\d\d-[01]\d-[0-3]\d\b`)

// timeOfDay follows the day of a timestamp written with a space ("2026-10-13 01:20").
var timeOfDay = regexp.MustCompile(`^,? \d\d?:\d\d`)

// layout is Go's reference date, which time layouts spell.
const layout = "2006-01-02"

func main() {
	roots := os.Args[1:]
	if len(roots) == 0 {
		roots = []string{"."}
	}
	found := 0
	for _, root := range roots {
		n, err := check(root)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		found += n
	}
	if found > 0 {
		fmt.Fprintf(os.Stderr, "%d dates in Go comments: comments state the current code, git holds when (a date that is data goes in quotes)\n", found)
		os.Exit(1)
	}
}

func check(root string) (int, error) {
	found := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		n, err := checkFile(path)
		found += n
		return err
	})
	return found, err
}

// checkFile prints the dates in path's comments and returns how many it found.
func checkFile(path string) (int, error) {
	src, err := os.ReadFile(path) // #nosec G304 -- a .go file of the tree being checked
	if err != nil {
		return 0, err
	}
	fset := token.NewFileSet()
	file := fset.AddFile(path, -1, len(src))
	var s scanner.Scanner
	var scanErr error
	s.Init(file, src, func(pos token.Position, msg string) {
		if scanErr == nil {
			scanErr = fmt.Errorf("%s: %s", pos, msg)
		}
	}, scanner.ScanComments)
	found := 0
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok != token.COMMENT {
			continue
		}
		start := file.Offset(pos)
		for _, m := range date.FindAllStringIndex(lit, -1) {
			lineStart := strings.LastIndexByte(lit[:m[0]], '\n') + 1
			line := lit[lineStart:]
			if i := strings.IndexByte(line, '\n'); i >= 0 {
				line = line[:i]
			}
			if isData(line, m[0]-lineStart, m[1]-lineStart) {
				continue
			}
			p := fset.Position(file.Pos(start + m[0]))
			fmt.Printf("%s:%d:%d: %s\n", path, p.Line, p.Column, strings.TrimSpace(line))
			found++
		}
	}
	return found, scanErr
}

// isData reports whether the date at line[from:to] is data: Go's reference date, the day of a
// timestamp, or inside a quoted string of its line.
func isData(line string, from, to int) bool {
	if line[from:to] == layout || timeOfDay.MatchString(line[to:]) {
		return true
	}
	return strings.Count(line[:from], `"`)%2 == 1 && strings.Contains(line[to:], `"`)
}
