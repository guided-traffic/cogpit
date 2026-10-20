package termview

import (
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

// Once the window closes (Terminal.Stop), the widget's output ends with io.EOF at the next read,
// and what that read brought, and the end notice, reach the widget no more (D-103).
func TestStopGateEndsTheOutput(t *testing.T) {
	var stopping atomic.Bool
	g := stopGate{src: &endNotice{src: strings.NewReader("first second"), notice: []byte("ended")}, stopping: &stopping}
	buf := make([]byte, 5)
	if n, err := g.Read(buf); err != nil || string(buf[:n]) != "first" {
		t.Fatalf("before Stop: %q, %v", buf[:n], err)
	}
	stopping.Store(true)
	if n, err := g.Read(buf); n != 0 || err != io.EOF {
		t.Errorf("after Stop: %d bytes, %v", n, err)
	}
	if n, err := g.Read(buf); n != 0 || err != io.EOF {
		t.Errorf("again: %d bytes, %v", n, err)
	}
}
