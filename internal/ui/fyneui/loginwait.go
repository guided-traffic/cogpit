package fyneui

import (
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
	"github.com/guided-traffic/cogpit/internal/model"
)

// loginWaitLine says what a connection waits for while its credential plugin runs (D-73; the
// engine's Connection.WaitingForLogin, as agents read it in ui_state): the program, since when, and
// when Cogpit ends it; for a run Cogpit stopped waiting for and could not end (RunsOn, H-118), that
// it has no end time. "" while none runs. The dots' tooltips (conndot.go) show it; the connecting
// workspace's placeholder its two sentences (tableview.go).
func loginWaitLine(w engine.LoginWait, tf model.TimeFormat) string {
	what, end := loginWaitSentences(w, tf)
	if what == "" {
		return ""
	}
	return what + " " + end
}

// loginWaitSentences are loginWaitLine's two sentences: what the connection waits for, and when it
// ends.
func loginWaitSentences(w engine.LoginWait, tf model.TimeFormat) (what, end string) {
	program := kubeconfig.DisplayText(w.Program, maxShownArg)
	switch {
	case w.Program == "":
		return "", ""
	case w.RunsOn:
		return "Waiting behind the login through " + program + ", running since " + tf.FormatClock(w.Since) +
				", which Cogpit stopped waiting for and could not end.",
			"It has no end time: it lasts until its process ends."
	}
	return "Waiting for the login through " + program + " since " + tf.FormatClock(w.Since) + "; it may wait for you in your browser.",
		"Cogpit ends it at " + tf.FormatClock(w.Until) + "."
}

// loginWaitCancel says what Cancel Connecting (place: " in its menu", or "" beside its button) does
// to a connect's wait: it ends the run, or, behind a run Cogpit could not end, only stops the wait.
func loginWaitCancel(w engine.LoginWait, place string) string {
	if w.RunsOn {
		return "Cancel Connecting" + place + " stops this wait; that login goes on."
	}
	return "Cancel Connecting" + place + " ends it now."
}
