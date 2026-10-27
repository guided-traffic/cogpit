package fyneui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// handleRevert answers editor.revert and editor.undoRevert as the engine does: the object as the
// cluster holds it (the fake's base) becomes the text, and the replaced text can come back once
// while the text is unchanged since.
func handleRevert(ed *editorFake) {
	undo := map[string]string{}
	undoRev := map[string]uint64{}
	params := func(inv engine.Invocation) (dockParams, error) {
		var p dockParams
		return p, json.Unmarshal(normalize(inv.Params), &p)
	}
	ed.f.handlers["editor.revert"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		p, err := params(inv)
		if err != nil {
			return nil, err
		}
		ed.mu.Lock()
		defer ed.mu.Unlock()
		changed := ed.texts[p.ID] != ed.base
		if changed {
			undo[p.ID] = ed.texts[p.ID]
			ed.texts[p.ID] = ed.base
			ed.revs[p.ID]++
			undoRev[p.ID] = ed.revs[p.ID]
		}
		canUndo := undo[p.ID] != "" && undoRev[p.ID] == ed.revs[p.ID]
		return engine.RevertResult{Ref: checkoutRef, Changed: changed, Text: ed.base, Revision: ed.revs[p.ID], BaseResourceVersion: "41", CanUndo: canUndo}, nil
	}
	ed.f.handlers["editor.undoRevert"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		p, err := params(inv)
		if err != nil {
			return nil, err
		}
		ed.mu.Lock()
		defer ed.mu.Unlock()
		if undo[p.ID] == "" || undoRev[p.ID] != ed.revs[p.ID] {
			return nil, engine.ErrNothingToUndo
		}
		ed.texts[p.ID] = undo[p.ID]
		delete(undo, p.ID)
		ed.revs[p.ID]++
		return engine.RevertResult{Ref: checkoutRef, Changed: true, Text: ed.texts[p.ID], Revision: ed.revs[p.ID], BaseResourceVersion: "41"}, nil
	}
}

// TestEditTabRevertsAndUndoes: Revert to Original sends the typing first, then puts the object as
// the cluster holds it into the editor; Undo Revert brings the typing back and goes; typing hides
// it too.
func TestEditTabRevertsAndUndoes(t *testing.T) {
	_, _, q, f, ed, et := editScene(t, ThemeDark)
	handleRevert(ed)
	if et.undoBtn.Visible() || !et.revertBtn.Visible() {
		t.Fatal("the footer starts with Revert to Original and without Undo Revert")
	}
	typeLineAfter(et.pane, 16, "sessionAffinity: ClientIP")
	edited := et.pane.view.Text()
	test.Tap(et.revertBtn)
	q.until(t, "the revert", func() bool { return et.pane.view.Text() == serviceYAML && !et.busy })
	calls := f.callsOf("editor.setText")
	if len(calls) == 0 || decodeParams[editorTextParams](t, calls[len(calls)-1]).YAML != edited {
		t.Fatal("the typing was not sent before the revert")
	}
	if !et.undoBtn.Visible() || !strings.Contains(et.status.text, "Reverted to Service checkout as the cluster holds it") {
		t.Errorf("after the revert: undo visible %v, status %q", et.undoBtn.Visible(), et.status.text)
	}

	test.Tap(et.undoBtn)
	q.until(t, "the undo", func() bool { return et.pane.view.Text() == edited && !et.busy })
	if et.undoBtn.Visible() || !strings.Contains(et.status.text, "Your text is back") {
		t.Errorf("after the undo: undo visible %v, status %q", et.undoBtn.Visible(), et.status.text)
	}

	test.Tap(et.revertBtn)
	q.until(t, "the second revert", func() bool { return et.undoBtn.Visible() && !et.busy })
	typeAt(et.pane, 0, 0, "# mine\n")
	if et.undoBtn.Visible() {
		t.Error("Undo Revert stays after typing")
	}
}

// TestEditTabUndoShortcuts: ⌘Z undoes the typing in the editor and ⌘Y redoes it, the engine
// following; after a Revert, whose text clears the editor's history, ⌘Z is Undo Revert, which
// brings the text back with the version it was based on (D-84).
func TestEditTabUndoShortcuts(t *testing.T) {
	_, _, q, f, ed, et := editScene(t, ThemeDark)
	handleRevert(ed)
	v := et.pane.view
	typeLineAfter(et.pane, 16, "sessionAffinity: ClientIP")
	edited := v.Text()
	v.TypedShortcut(&fyne.ShortcutUndo{})
	if v.Text() != serviceYAML {
		t.Fatalf("⌘Z left\n%s", v.Text())
	}
	v.TypedShortcut(&fyne.ShortcutRedo{})
	if v.Text() != edited {
		t.Fatal("⌘Y did not bring the line back")
	}
	q.until(t, "the redone text sent", func() bool {
		calls := f.callsOf("editor.setText")
		return len(calls) > 0 && decodeParams[editorTextParams](t, calls[len(calls)-1]).YAML == edited
	})

	test.Tap(et.revertBtn)
	q.until(t, "the revert", func() bool { return v.Text() == serviceYAML && et.undoBtn.Visible() && !et.busy })
	v.TypedShortcut(&fyne.ShortcutUndo{}) // nothing in the editor's history: Undo Revert
	q.until(t, "the undo of the revert", func() bool { return v.Text() == edited && !et.busy })
	if et.undoBtn.Visible() || !strings.Contains(et.status.text, "Your text is back") {
		t.Errorf("after ⌘Z: undo visible %v, status %q", et.undoBtn.Visible(), et.status.text)
	}
	n := len(f.callsOf("editor.undoRevert"))
	v.TypedShortcut(&fyne.ShortcutUndo{}) // nothing to undo and no Undo Revert offered: nothing
	if v.Text() != edited || len(f.callsOf("editor.undoRevert")) != n {
		t.Error("a second ⌘Z changed the text or asked the engine")
	}
}

// TestReviewRevertsToTheEditor: Revert to Original in a Save's review reverts and leaves the review.
func TestReviewRevertsToTheEditor(t *testing.T) {
	_, _, q, _, ed, et := editScene(t, ThemeDark)
	handleRevert(ed)
	typeLineAfter(et.pane, 16, "sessionAffinity: ClientIP")
	test.Tap(et.saveBtn)
	q.until(t, "the review", func() bool { return et.reviewing && !et.applyBtn.Disabled() })
	if !et.reviewRevert.Visible() {
		t.Fatal("the review has no Revert to Original")
	}
	test.Tap(et.reviewRevert)
	q.until(t, "the revert", func() bool { return !et.reviewing && et.pane.view.Text() == serviceYAML })
	if !et.undoBtn.Visible() {
		t.Error("no Undo Revert after reverting the reviewed changes")
	}
	// Nothing left to revert: the status says so.
	test.Tap(et.revertBtn)
	q.until(t, "the second revert", func() bool { return strings.HasPrefix(et.status.text, "Nothing to revert") })

	ed.f.handlers["editor.undoRevert"] = func(context.Context, engine.Invocation) (any, error) {
		return nil, errors.New("the text changed since")
	}
	test.Tap(et.undoBtn)
	q.until(t, "the refused undo", func() bool { return strings.HasPrefix(et.status.text, "Not undone") })
}

func renderEditRevert(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _, ed, et := editScene(t, mode)
	handleRevert(ed)
	setDockOffset(sh, renderDockOffset)
	typeLineAfter(et.pane, 16, "sessionAffinity: ClientIP")
	test.Tap(et.revertBtn)
	q.until(t, "the revert", func() bool { return et.undoBtn.Visible() && !et.busy })
	savePNG(t, w, name)
}

func TestRenderEditRevertDark(t *testing.T) { renderEditRevert(t, ThemeDark, "edit_revert_dark.png") }
