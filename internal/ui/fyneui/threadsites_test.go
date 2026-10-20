package fyneui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// threadSitesDoc is the audit of every place where the GUI's code runs off Fyne's main goroutine
// (D-103): releases build Fyne's thread check out, so a Fyne call from such a place races instead
// of being moved to the main goroutine.
const threadSitesDoc = "../../../docs/developer/ui-threads.md"

// threadSite is a place found in the code: a file below internal/ui, the function it is in
// ("Type.method" or "func"), and how it leaves the main goroutine.
type threadSite struct{ file, fn, kind string }

func (s threadSite) String() string { return s.file + " " + s.fn + " " + s.kind }

// The kinds the code is scanned for; the audit's other kinds (callback, wait) are found by reading.
var scannedKinds = map[string]bool{"go": true, "AfterFunc": true, "Ticker": true, "Timer": true, "After": true, "cgo": true}

// TestEveryGoroutineSiteIsAudited: every go statement, timer and cgo export in internal/ui has its
// row in the audit, and every scanned row still names one in the code. A new goroutine fails here
// until someone checked that it touches Fyne only through fyne.Do (sh.do) and wrote down why.
func TestEveryGoroutineSiteIsAudited(t *testing.T) {
	inCode := scanThreadSites(t, "..")
	inDoc := auditedThreadSites(t)
	if len(inDoc) == 0 {
		t.Fatalf("%s lists no sites", threadSitesDoc)
	}
	var missing, stale []string
	for s := range inCode {
		if !inDoc[s] {
			missing = append(missing, s.String())
		}
	}
	for s := range inDoc {
		if scannedKinds[s.kind] && !inCode[s] {
			stale = append(stale, s.String())
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	for _, s := range missing {
		t.Errorf("not audited: %s — add its row to %s (does it touch Fyne only through sh.do or fyne.Do?)", s, threadSitesDoc)
	}
	for _, s := range stale {
		t.Errorf("audited but not in the code any more: %s — remove or rename its row in %s", s, threadSitesDoc)
	}
}

// scanThreadSites finds the sites in the non-test Go files below root (internal/ui).
func scanThreadSites(t *testing.T, root string) map[threadSite]bool {
	t.Helper()
	sites := map[threadSite]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			name := funcName(fd)
			if fd.Doc != nil {
				for _, c := range fd.Doc.List {
					if strings.HasPrefix(c.Text, "//export ") {
						sites[threadSite{rel, name, "cgo"}] = true
					}
				}
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.GoStmt:
					sites[threadSite{rel, name, "go"}] = true
				case *ast.CallExpr:
					if kind := timeKind(n); kind != "" {
						sites[threadSite{rel, name, kind}] = true
					}
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return sites
}

// funcName is "Type.method" for a method, else the function's name.
func funcName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	typ := fd.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	if idx, ok := typ.(*ast.IndexExpr); ok { // a generic receiver
		typ = idx.X
	}
	if id, ok := typ.(*ast.Ident); ok {
		return id.Name + "." + fd.Name.Name
	}
	return fd.Name.Name
}

// timeKind names a call of package time that runs something later on its own goroutine or feeds a
// goroutine's wait: AfterFunc, a ticker, a timer, After.
func timeKind(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "time" {
		return ""
	}
	switch sel.Sel.Name {
	case "AfterFunc":
		return "AfterFunc"
	case "NewTicker", "Tick":
		return "Ticker"
	case "NewTimer":
		return "Timer"
	case "After":
		return "After"
	}
	return ""
}

// auditedThreadSites reads the audit's tables: rows "| file | function | kind | class | why |".
func auditedThreadSites(t *testing.T) map[threadSite]bool {
	t.Helper()
	b, err := os.ReadFile(threadSitesDoc)
	if err != nil {
		t.Fatal(err)
	}
	row := regexp.MustCompile("^\\|\\s*`?((?:fyneui|nativedialog|logo)/[^`|\\s]+\\.go)`?\\s*\\|\\s*`?([^`|]+?)`?\\s*\\|\\s*`?([A-Za-z]+)`?\\s*\\|\\s*([A-E])\\b")
	sites := map[threadSite]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		m := row.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		sites[threadSite{m[1], m[2], m[3]}] = true
	}
	return sites
}
