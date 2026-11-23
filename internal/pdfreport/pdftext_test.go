package pdfreport

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"io"
	"regexp"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/signintech/gopdf"
)

// pdfDoc is what the tests read back from a rendered document: its streams, inflated.
type pdfDoc struct {
	raw     []byte
	streams [][]byte
	// decoded is the text, once per font's map (see decode).
	decoded []string
}

var streamRE = regexp.MustCompile(`(?s)stream\r?\n(.*?)\r?\nendstream`)

func readPDF(t *testing.T, b []byte) pdfDoc {
	t.Helper()
	d := pdfDoc{raw: b}
	for _, m := range streamRE.FindAllSubmatch(b, -1) {
		data := m[1]
		if zr, err := zlib.NewReader(bytes.NewReader(data)); err == nil {
			if out, err := io.ReadAll(zr); err == nil {
				data = out
			}
		}
		d.streams = append(d.streams, data)
	}
	d.decoded = d.decode()
	return d
}

// pages counts the page objects.
func (d pdfDoc) pages() int {
	return len(pageRE.FindAll(d.raw, -1))
}

var (
	bfrangeRE = regexp.MustCompile(`<([0-9A-F]{4})><([0-9A-F]{4})><([0-9A-F]+)>`)
	tjRE      = regexp.MustCompile(`\[<([0-9A-F<>\-0-9]*)>\] TJ`)
	kernRE    = regexp.MustCompile(`>-?\d+<`)
	pageRE    = regexp.MustCompile(`/Type /Page\b[^s]`)
)

// decode decodes the documents' text runs with every font's ToUnicode map in turn and returns one
// decoding per font, each run on its own line: the runs of a font decode right with its map.
func (d pdfDoc) decode() []string {
	var maps []map[string]string
	for _, s := range d.streams {
		if !bytes.Contains(s, []byte("beginbfrange")) {
			continue
		}
		m := map[string]string{}
		for _, e := range bfrangeRE.FindAllSubmatch(s, -1) {
			u, err := hex.DecodeString(string(e[3]))
			if err != nil {
				continue
			}
			var units []uint16
			for i := 0; i+1 < len(u); i += 2 {
				units = append(units, uint16(u[i])<<8|uint16(u[i+1]))
			}
			m[string(e[1])] = string(utf16.Decode(units))
		}
		maps = append(maps, m)
	}
	var runs []string
	for _, s := range d.streams {
		for _, e := range tjRE.FindAllSubmatch(s, -1) {
			runs = append(runs, string(e[1]))
		}
	}
	var out []string
	for _, m := range maps {
		var b strings.Builder
		for _, run := range runs {
			// A run may hold kerning numbers between hex groups: ">-12<".
			for _, part := range kernRE.Split(run, -1) {
				for i := 0; i+4 <= len(part); i += 4 {
					b.WriteString(m[part[i:i+4]])
				}
			}
			b.WriteByte('\n')
		}
		out = append(out, b.String())
	}
	return out
}

// contains reports whether one font's decoding holds s in one text run.
func (d pdfDoc) contains(s string) bool {
	for _, txt := range d.decoded {
		if strings.Contains(txt, s) {
			return true
		}
	}
	return false
}

func TestReadPDFPages(t *testing.T) {
	b, err := Render(ImageSecurity{Cluster: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if n := readPDF(t, b).pages(); n != 1 {
		t.Fatalf("pages = %d, want 1", n)
	}
}

func pdfInfoForTest() gopdf.PdfInfo { return gopdf.PdfInfo{Title: "test"} }
