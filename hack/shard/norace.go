package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
)

// raceMarker is the constant a package's tests read to tell whether they run under the race
// detector (race_on_test.go and race_off_test.go define it per package).
const raceMarker = "raceEnabled"

// packageTests is a package's tests that the race detector keeps from running.
type packageTests struct {
	importPath string
	names      []string
}

// raceKept lists, for every package of the pattern (go list's, such as ./...), the top-level tests
// the race detector keeps from running, fully or in part, so that a job without it runs them (make
// test-norace-coverage): the tests only the build without the race tag compiles (a //go:build !race
// file), and the tests that read raceMarker, directly or through anything of their test package that
// does (a helper function or method, a function value, a package-level variable or constant set
// from it: skipTerminalUnderRace in internal/ui/fyneui). Names link by name within one test package
// (the package and its _test package apart), so a test can be selected that need not be: it then
// runs once more without the race detector, which costs little and misses nothing. Packages without
// such tests are left out.
func raceKept(pattern string) ([]packageTests, error) {
	plain, err := listPackages(pattern, "")
	if err != nil {
		return nil, err
	}
	race, err := listPackages(pattern, "race")
	if err != nil {
		return nil, err
	}
	underRace := map[string][]string{}
	for _, p := range race {
		files, err := p.parseTests()
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			for _, d := range f.Decls {
				if fn, ok := d.(*ast.FuncDecl); ok && isTopLevelTest(fn) {
					underRace[p.ImportPath] = append(underRace[p.ImportPath], fn.Name.Name)
				}
			}
		}
	}
	var out []packageTests
	for _, p := range plain {
		files, err := p.parseTests()
		if err != nil {
			return nil, err
		}
		if names := keptIn(files, underRace[p.ImportPath]); len(names) > 0 {
			out = append(out, packageTests{importPath: p.ImportPath, names: names})
		}
	}
	return out, nil
}

// testPackage is what go list -json says of a package's test files.
type testPackage struct {
	ImportPath   string
	Dir          string
	TestGoFiles  []string
	XTestGoFiles []string
}

// listPackages runs go list -json for the pattern with the given build tags.
func listPackages(pattern, tags string) ([]testPackage, error) {
	args := []string{"list", "-json"}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	out, err := exec.Command("go", append(args, pattern)...).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("go list %s: %v: %s", pattern, err, ee.Stderr)
		}
		return nil, fmt.Errorf("go list %s: %w", pattern, err)
	}
	var pkgs []testPackage
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p testPackage
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			return pkgs, nil
		} else if err != nil {
			return nil, fmt.Errorf("go list %s: %w", pattern, err)
		}
		pkgs = append(pkgs, p)
	}
}

// parseTests parses the package's test files, its own and its _test package's.
func (p testPackage) parseTests() ([]*ast.File, error) {
	fset := token.NewFileSet()
	var files []*ast.File
	for _, f := range append(slices.Clone(p.TestGoFiles), p.XTestGoFiles...) {
		file, err := parser.ParseFile(fset, filepath.Join(p.Dir, f), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

// declKey names a function, method, variable or constant within one test package.
type declKey struct{ pkg, name string }

// keptIn is the sorted top-level tests of the files (one build's test files of a package) that the
// race detector keeps from running, given the tests the race build compiles.
func keptIn(files []*ast.File, underRace []string) []string {
	bodies := map[declKey][]ast.Node{} // what each declaration reads: a function's body, a value's expressions
	tests := map[declKey]bool{}
	for _, file := range files {
		pkg := file.Name.Name
		for _, d := range file.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Body == nil {
					continue
				}
				k := declKey{pkg, d.Name.Name}
				bodies[k] = append(bodies[k], d.Body)
				if isTopLevelTest(d) {
					tests[k] = true
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if vs, ok := spec.(*ast.ValueSpec); ok {
						for _, name := range vs.Names {
							k := declKey{pkg, name.Name}
							for _, v := range vs.Values {
								bodies[k] = append(bodies[k], v)
							}
						}
					}
				}
			}
		}
	}
	reads := readsMarker(bodies)
	var names []string
	for k := range tests {
		if reads[k] || !slices.Contains(underRace, k.name) {
			names = append(names, k.name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// readsMarker reports for each declaration whether what it reads names raceMarker, or names a
// declaration of its own test package that does (called or not; a method through a selector too, as
// any declaration of that name), to the fixpoint.
func readsMarker(bodies map[declKey][]ast.Node) map[declKey]bool {
	reads := map[declKey]bool{}
	refs := map[declKey][]declKey{}
	for k, nodes := range bodies {
		for _, n := range nodes {
			ast.Inspect(n, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident) // a selector's name (x.method) is an identifier too
				if !ok {
					return true
				}
				if id.Name == raceMarker {
					reads[k] = true
				} else if o := (declKey{k.pkg, id.Name}); o != k && bodies[o] != nil {
					refs[k] = append(refs[k], o)
				}
				return true
			})
		}
	}
	for changed := true; changed; {
		changed = false
		for k, targets := range refs {
			if reads[k] {
				continue
			}
			for _, o := range targets {
				if reads[o] {
					reads[k], changed = true, true
					break
				}
			}
		}
	}
	return reads
}
