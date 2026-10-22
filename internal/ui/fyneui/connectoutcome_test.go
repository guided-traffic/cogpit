package fyneui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The owner's AW15 and AW16 (alpha walk, 2026-10-22): his own Cancel Connecting and a connect that
// goes on in the background once he showed another cluster each raised an error toast in the Go
// error's form. A connect that did not fail raises no toast, on every way the window answers a
// command (a dispatch without its own answer, a hotbar tile's and the start's --context); a connect
// that failed still says why.
func TestConnectOutcomesThatAreNoFailureRaiseNoToast(t *testing.T) {
	f := connectedFake(t, 3)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	const name = "walk-login-hang [walk-kube/walk-login.yaml]"
	var answer error
	f.mu.Lock()
	f.handlers["cluster.connect"] = func(context.Context, engine.Invocation) (any, error) { return nil, answer }
	f.mu.Unlock()
	params := json.RawMessage(`{"context":"` + name + `"}`)
	for _, err := range []error{
		fmt.Errorf("connect %s: %w", name, engine.ErrConnectCancelled),
		fmt.Errorf("connect %s: %w", name, engine.ErrConnectInBackground),
		fmt.Errorf("connect %s: %w", name, engine.ErrConnectSuperseded),
	} {
		f.mu.Lock()
		answer = err
		f.mu.Unlock()
		sh.run("cluster.connect", params, nil) // as dispatch runs it, on this goroutine
		sh.showError(err)                      // a hotbar tile's answer (hotbar.go open)
		sh.autoConnected(name)(nil, err)       // the start's --context
		q.drain()
		if got := toastTexts(sh); got != "" {
			t.Errorf("%v raised a toast: %q", err, got)
		}
	}

	failed := errors.New("connect " + name + ": the login through walk-login.sh did not finish within 5 minutes; Connect starts it again")
	f.mu.Lock()
	answer = failed
	f.mu.Unlock()
	sh.run("cluster.connect", params, nil)
	q.drain()
	if got := toastTexts(sh); !strings.Contains(got, "did not finish within 5 minutes") {
		t.Errorf("a failed connect's toast: %q", got)
	}
	sh.autoConnected(name)(nil, failed)
	q.drain()
	if got := toastTexts(sh); !strings.Contains(got, "Could not connect to "+name) {
		t.Errorf("a failed --context connect's toast: %q", got)
	}
}
