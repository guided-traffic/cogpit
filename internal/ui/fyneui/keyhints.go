package fyneui

// keyHint is one line of the key overview (`?`, keysoverlay.go): the keys as the overview shows
// them and what they do. A place that takes keys of its own while it has the keyboard (the logs
// tab, the table) lists them as keyHints, so that the overview shows what acts now and never goes
// stale.
type keyHint struct {
	Keys  string // as the overview shows them: "t", "2–6", "⌘S"
	Label string // what they do: "Timestamps on or off"
}
