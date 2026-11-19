package engine

// The command palette's typed text (D-51, the owner's A54, 2026-10-14 evening) is engine state, as
// the navigator's search is: UIState.PaletteText, set by the window as the user types and by
// ui.palette.setText, which an agent runs freely (D-2: a UI command).
//
// An agent must not choose the entry the user's next ↵ runs: the text decides which entry is
// highlighted first. So UIState.PaletteTextOrigin says who set the text last, and the window
// (fyneui/palette.go) runs a change on ↵, a mutating or destructive entry, only when the user typed
// the text or picked the entry with the arrow keys; on a text an agent set, ↵ runs reads and UI
// entries, and a change needs the user's arrow key or click. (The lead's night decision, 2026-10-14,
// for the owner to confirm; the options are recorded in D-51.) The palette opened by an agent does
// not take the keyboard focus either, so an agent's text never meets the user's
// typing unasked.

// SetPaletteText sets the command palette's typed text (ui.palette.setText) as origin, opening the
// palette when it is closed, and returns the new state. The window's own report (origin gui) never
// opens it: one that arrives after the user closed the palette is dropped.
func (s *Session) SetPaletteText(text string, origin Origin) (UIState, error) {
	if err := CheckNavigatorSearch(text); err != nil {
		return s.UIState(), err
	}
	if !origin.valid() {
		origin = OriginGUI
	}
	return s.UpdateUI(func(u *UIState) {
		if origin == OriginGUI && !u.Palette {
			return
		}
		u.Palette, u.PaletteText, u.PaletteTextOrigin = true, text, origin
		if text == "" {
			u.PaletteTextOrigin = ""
		}
	}), nil
}

// PaletteTextByOther reports whether the palette's text was set last by someone other than the
// window's user: an agent, the CLI or a link.
func (u UIState) PaletteTextByOther() bool {
	return u.PaletteText != "" && u.PaletteTextOrigin != "" && u.PaletteTextOrigin != OriginGUI
}
