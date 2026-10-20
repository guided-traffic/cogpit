package terminal

import (
	"bytes"
	"io"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	widget2 "github.com/fyne-io/terminal/internal/widget"
	"github.com/stretchr/testify/assert"
)

// NopCloser returns a WriteCloser with a no-op Close method wrapping
// the provided writer w.
func NopCloser(w io.Writer) io.WriteCloser {
	return nopCloser{w}
}

type nopCloser struct {
	io.Writer
}

func (n nopCloser) Close() error {
	return nil
}

// setKeyModifiers makes keyModifiers report ⇧ while shift is set, as the desktop driver does while a
// key typed with ⇧ is handled. Cogpit patch.
func setKeyModifiers(t *testing.T, shift bool) {
	t.Helper()
	saved := keyModifiers
	t.Cleanup(func() { keyModifiers = saved })
	keyModifiers = func() fyne.KeyModifier {
		if shift {
			return fyne.KeyModifierShift
		}
		return 0
	}
}

func TestTerminal_TypedKey(t *testing.T) {
	tests := map[string]struct {
		key          fyne.KeyName
		bufferMode   bool
		shiftPressed bool
		want         []byte
	}{
		"F1":  {fyne.KeyF1, false, false, []byte{asciiEscape, 'O', 'P'}},
		"F2":  {fyne.KeyF2, false, false, []byte{asciiEscape, 'O', 'Q'}},
		"F3":  {fyne.KeyF3, false, false, []byte{asciiEscape, 'O', 'R'}},
		"F4":  {fyne.KeyF4, false, false, []byte{asciiEscape, 'O', 'S'}},
		"F5":  {fyne.KeyF5, false, false, []byte{asciiEscape, '[', '1', '5', '~'}},
		"F6":  {fyne.KeyF6, false, false, []byte{asciiEscape, '[', '1', '7', '~'}},
		"F7":  {fyne.KeyF7, false, false, []byte{asciiEscape, '[', '1', '8', '~'}},
		"F8":  {fyne.KeyF8, false, false, []byte{asciiEscape, '[', '1', '9', '~'}},
		"F9":  {fyne.KeyF9, false, false, []byte{asciiEscape, '[', '2', '0', '~'}},
		"F10": {fyne.KeyF10, false, false, []byte{asciiEscape, '[', '2', '1', '~'}},
		"F11": {fyne.KeyF11, false, false, []byte{asciiEscape, '[', '2', '3', '~'}},
		"F12": {fyne.KeyF12, false, false, []byte{asciiEscape, '[', '2', '4', '~'}},
		// Cogpit patch: xterm's sequences with ⇧ (the terminfo entry xterm-256color); upstream sent
		// ESC [ 25 ~, ESC [ 26 ~ and ESC O R ; 2 ~ for ⇧F1 to ⇧F3, ESC [ A ; 2 for ⇧↑, and nothing for
		// ⇧Tab, ⇧Return and the other keys without a form of their own.
		"Shift+F1":        {fyne.KeyF1, false, true, []byte("\x1b[1;2P")},
		"Shift+F2":        {fyne.KeyF2, false, true, []byte("\x1b[1;2Q")},
		"Shift+F3":        {fyne.KeyF3, false, true, []byte("\x1b[1;2R")},
		"Shift+F4":        {fyne.KeyF4, false, true, []byte("\x1b[1;2S")},
		"Shift+F5":        {fyne.KeyF5, false, true, []byte("\x1b[15;2~")},
		"Shift+F6":        {fyne.KeyF6, false, true, []byte("\x1b[17;2~")},
		"Shift+F7":        {fyne.KeyF7, false, true, []byte("\x1b[18;2~")},
		"Shift+F8":        {fyne.KeyF8, false, true, []byte("\x1b[19;2~")},
		"Shift+F9":        {fyne.KeyF9, false, true, []byte("\x1b[20;2~")},
		"Shift+F10":       {fyne.KeyF10, false, true, []byte("\x1b[21;2~")},
		"Shift+F11":       {fyne.KeyF11, false, true, []byte("\x1b[23;2~")},
		"Shift+F12":       {fyne.KeyF12, false, true, []byte("\x1b[24;2~")},
		"Shift+PageUp":    {fyne.KeyPageUp, false, true, []byte("\x1b[5;2~")},
		"Shift+PageDown":  {fyne.KeyPageDown, false, true, []byte("\x1b[6;2~")},
		"Shift+Home":      {fyne.KeyHome, false, true, []byte("\x1b[1;2H")},
		"Shift+Insert":    {fyne.KeyInsert, false, true, []byte("\x1b[2;2~")},
		"Shift+Delete":    {fyne.KeyDelete, false, true, []byte("\x1b[3;2~")},
		"Shift+End":       {fyne.KeyEnd, false, true, []byte("\x1b[1;2F")},
		"Shift+Up":        {fyne.KeyUp, false, true, []byte("\x1b[1;2A")},
		"Shift+Down":      {fyne.KeyDown, false, true, []byte("\x1b[1;2B")},
		"Shift+Left":      {fyne.KeyLeft, false, true, []byte("\x1b[1;2D")},
		"Shift+Right":     {fyne.KeyRight, false, true, []byte("\x1b[1;2C")},
		"Shift+Up, ESC O": {fyne.KeyUp, true, true, []byte("\x1b[1;2A")}, // with ⇧ always CSI, as xterm
		"Shift+Tab":       {fyne.KeyTab, false, true, []byte("\x1b[Z")},
		"Shift+Return":    {fyne.KeyReturn, false, true, []byte{'\r'}},
		"Shift+Enter":     {fyne.KeyEnter, false, true, []byte{'\n'}},
		"Shift+Backspace": {fyne.KeyBackspace, false, true, []byte{asciiBackspace}},
		"Shift+Escape":    {fyne.KeyEscape, false, true, []byte{asciiEscape}},

		"PageUp":    {fyne.KeyPageUp, false, false, []byte{asciiEscape, '[', '5', '~'}},
		"PageDown":  {fyne.KeyPageDown, false, false, []byte{asciiEscape, '[', '6', '~'}},
		"Home":      {fyne.KeyHome, false, false, []byte{asciiEscape, 'O', 'H'}},
		"Insert":    {fyne.KeyInsert, false, false, []byte{asciiEscape, '[', '2', '~'}},
		"Delete":    {fyne.KeyDelete, false, false, []byte{asciiEscape, '[', '3', '~'}},
		"End":       {fyne.KeyEnd, false, false, []byte{asciiEscape, 'O', 'F'}},
		"Enter":     {fyne.KeyEnter, false, false, []byte{'\n'}}, // Modify as needed for Windows or bufferMode
		"Tab":       {fyne.KeyTab, false, false, []byte{'\t'}},
		"Escape":    {fyne.KeyEscape, false, false, []byte{asciiEscape}},
		"Backspace": {fyne.KeyBackspace, false, false, []byte{asciiBackspace}},
		"Up":        {fyne.KeyUp, false, false, []byte{asciiEscape, '[', 'A'}},
		"Down":      {fyne.KeyDown, false, false, []byte{asciiEscape, '[', 'B'}},
		"Left":      {fyne.KeyLeft, false, false, []byte{asciiEscape, '[', 'D'}},
		"Right":     {fyne.KeyRight, false, false, []byte{asciiEscape, '[', 'C'}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// Creating a mock terminal
			inBuffer := bytes.NewBuffer([]byte{})
			term := &Terminal{in: NopCloser(inBuffer), bufferMode: tt.bufferMode}
			setKeyModifiers(t, tt.shiftPressed) // Cogpit patch: ⇧ from the key's own event
			keyEvent := &fyne.KeyEvent{Name: tt.key}

			term.TypedKey(keyEvent)

			got := inBuffer.Bytes()
			if !bytes.Equal(got, tt.want) {
				t.Errorf("TypedKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTerminal_TypedKey_LineMode(t *testing.T) {
	tests := map[string]struct {
		key         fyne.KeyName
		newLineMode bool
		want        []byte
	}{
		"Enter":                 {fyne.KeyEnter, false, []byte{'\n'}},
		"Enter with line mode":  {fyne.KeyEnter, true, []byte{'\r'}},
		"Return":                {fyne.KeyReturn, false, []byte{'\r'}},
		"Return with line mode": {fyne.KeyReturn, true, []byte{'\r'}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// Creating a mock terminal
			inBuffer := bytes.NewBuffer([]byte{})
			term := &Terminal{in: NopCloser(inBuffer), newLineMode: tt.newLineMode}
			keyEvent := &fyne.KeyEvent{Name: tt.key}

			term.TypedKey(keyEvent)

			got := inBuffer.Bytes()
			if !bytes.Equal(got, tt.want) {
				t.Errorf("TypedKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTerminal_TypedShortcut(t *testing.T) {
	tests := map[string]struct {
		shortcut fyne.Shortcut
		want     []byte
	}{
		"LeftOption+U": {
			shortcut: &desktop.CustomShortcut{
				Modifier: fyne.KeyModifierAlt,
				KeyName:  fyne.KeyU,
			},
			want: []byte{},
		},
		"Control+@": {
			shortcut: &desktop.CustomShortcut{
				Modifier: fyne.KeyModifierControl,
				KeyName:  "@",
			},
			want: []byte{0},
		},
		"Control+Space": {
			shortcut: &desktop.CustomShortcut{
				Modifier: fyne.KeyModifierControl,
				KeyName:  fyne.KeySpace,
			},
			want: []byte{0},
		},
		"Control+C": {
			shortcut: &desktop.CustomShortcut{
				Modifier: fyne.KeyModifierControl,
				KeyName:  fyne.KeyC,
			},
			want: []byte{3},
		},
		"Control+_": {
			shortcut: &desktop.CustomShortcut{
				Modifier: fyne.KeyModifierControl,
				KeyName:  "_",
			},
			want: []byte{31},
		},
		"Control+X": {
			shortcut: &desktop.CustomShortcut{
				Modifier: fyne.KeyModifierControl,
				KeyName:  fyne.KeyX,
			},
			want: []byte{24},
		},
		// Cogpit patch: Ctrl with a named key sends xterm's sequence with the modifier parameter 5
		// (6 with ⇧), or nothing; it sent the control character of the name's first letter, so
		// Ctrl+↓ and Ctrl+Delete sent ^D (end of input: a shell at an empty prompt exits), Ctrl+↑
		// ^U, Ctrl+← ^L, Ctrl+→ and Ctrl+Return ^R, Ctrl+Home ^H.
		"Control+Left":        {&desktop.CustomShortcut{Modifier: fyne.KeyModifierControl, KeyName: fyne.KeyLeft}, []byte("\x1b[1;5D")},
		"Control+Down":        {&desktop.CustomShortcut{Modifier: fyne.KeyModifierControl, KeyName: fyne.KeyDown}, []byte("\x1b[1;5B")},
		"Control+Delete":      {&desktop.CustomShortcut{Modifier: fyne.KeyModifierControl, KeyName: fyne.KeyDelete}, []byte("\x1b[3;5~")},
		"Control+Home":        {&desktop.CustomShortcut{Modifier: fyne.KeyModifierControl, KeyName: fyne.KeyHome}, []byte("\x1b[1;5H")},
		"Control+F1":          {&desktop.CustomShortcut{Modifier: fyne.KeyModifierControl, KeyName: fyne.KeyF1}, []byte("\x1b[1;5P")},
		"Control+F5":          {&desktop.CustomShortcut{Modifier: fyne.KeyModifierControl, KeyName: fyne.KeyF5}, []byte("\x1b[15;5~")},
		"Control+Shift+Right": {&desktop.CustomShortcut{Modifier: fyne.KeyModifierControl | fyne.KeyModifierShift, KeyName: fyne.KeyRight}, []byte("\x1b[1;6C")},
		"Control+Return":      {&desktop.CustomShortcut{Modifier: fyne.KeyModifierControl, KeyName: fyne.KeyReturn}, []byte{}},
		"Control+Escape":      {&desktop.CustomShortcut{Modifier: fyne.KeyModifierControl, KeyName: fyne.KeyEscape}, []byte{}},
		"Control+Shift+C":     {&desktop.CustomShortcut{Modifier: fyne.KeyModifierControl | fyne.KeyModifierShift, KeyName: fyne.KeyC}, []byte{}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// Creating a mock terminal
			inBuffer := bytes.NewBuffer([]byte{})
			term := &Terminal{in: NopCloser(inBuffer)}

			term.TypedShortcut(tt.shortcut)

			got := inBuffer.Bytes()
			if !bytes.Equal(got, tt.want) {
				t.Errorf("TypedShortcut() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTerminal_TypedKey_ModifierHeldOnFocusChange(t *testing.T) {
	buf1 := bytes.NewBuffer([]byte{})
	term1 := New()
	term1.in = NopCloser(buf1)
	term2 := New()

	w := test.NewTempWindow(t, container.NewGridWithColumns(2, term1, term2))
	w.Canvas().Focus(term1)

	// a shortcut like ctrl+shift+T moves focus whilst the modifier is still down
	term1.KeyDown(&fyne.KeyEvent{Name: desktop.KeyShiftLeft})
	w.Canvas().Focus(term2)
	term2.KeyUp(&fyne.KeyEvent{Name: desktop.KeyShiftLeft})

	w.Canvas().Focus(term1)
	term1.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})

	got := buf1.Bytes()
	want := []byte{'\r'}
	if !bytes.Equal(got, want) {
		t.Errorf("TypedKey() = %v, want %v", got, want)
	}
}

// TestTypedKey_ShiftReleaseNeverSeen: Cogpit's G16 GW8 (the owner's gate walk of 2026-10-20). ⇧⌘4
// starts macOS's screenshot overlay, which takes the keys while the window stays key: the widget saw
// ⇧ go down and never come up. It kept ⇧ "held", sent ← and → as ESC [ D ; 2 and ESC [ C ; 2, which
// zsh reads as the arrow and the text ";2" (after a paste of "… | head -3", ← and → left
// "head -;23;2"), and sent nothing for Return, until a click outside the terminal and back reset it.
// Cogpit patch: a key takes its modifiers from its own event (keyModifiers), which has no ⇧ here.
func TestTypedKey_ShiftReleaseNeverSeen(t *testing.T) {
	test.NewTempApp(t) // its driver reports no modifiers, as a key without ⇧ does
	in := &bytes.Buffer{}
	term := &Terminal{in: NopCloser(in)}
	term.KeyDown(&fyne.KeyEvent{Name: desktop.KeyShiftLeft})
	term.KeyDown(&fyne.KeyEvent{Name: desktop.KeySuperLeft})
	// the 4 and every release go to the overlay
	for _, k := range []fyne.KeyName{fyne.KeyLeft, fyne.KeyRight, fyne.KeyReturn} {
		term.TypedKey(&fyne.KeyEvent{Name: k})
	}
	assert.Equal(t, "\x1b[D\x1b[C\r", in.String())
}

// modDriver is Fyne's test driver as a desktop driver whose key event being handled has mods.
type modDriver struct {
	fyne.Driver
	mods fyne.KeyModifier
}

func (d modDriver) CreateSplashWindow() fyne.Window       { return nil }
func (d modDriver) CurrentKeyModifiers() fyne.KeyModifier { return d.mods }
func (d modDriver) HasSecondaryDisplay() bool             { return false }

var _ desktop.Driver = modDriver{}

type modApp struct {
	fyne.App
	driver modDriver
}

func (a modApp) Driver() fyne.Driver { return a.driver }

// TestKeyModifiers_FromTheDesktopDriver: keyModifiers reads the desktop driver's modifiers, which
// are the key event's own while it is handled, so ⇧↑ typed for real goes as ESC [ 1 ; 2 A. Cogpit
// patch.
func TestKeyModifiers_FromTheDesktopDriver(t *testing.T) {
	base := test.NewTempApp(t)
	fyne.SetCurrentApp(modApp{App: base, driver: modDriver{Driver: base.Driver(), mods: fyne.KeyModifierShift}})
	t.Cleanup(func() { fyne.SetCurrentApp(base) })
	in := &bytes.Buffer{}
	term := &Terminal{in: NopCloser(in)}
	term.TypedKey(&fyne.KeyEvent{Name: fyne.KeyUp})
	assert.Equal(t, "\x1b[1;2A", in.String())
}

// TestTypedShortcut_ScreenshotKeysSendNothing: GW8's second suspect. Fyne hands ⇧⌘3, ⇧⌘4 and ⇧⌘5 to
// the focused terminal as shortcuts when they reach the window; like Terminal.app and iTerm2, the
// terminal sends nothing for a ⌘ combination (it never did; the test keeps it so).
func TestTypedShortcut_ScreenshotKeysSendNothing(t *testing.T) {
	test.NewTempApp(t)
	in := &bytes.Buffer{}
	term := &Terminal{in: NopCloser(in)}
	for _, k := range []fyne.KeyName{fyne.Key3, fyne.Key4, fyne.Key5} {
		term.TypedShortcut(&desktop.CustomShortcut{KeyName: k, Modifier: fyne.KeyModifierShift | fyne.KeyModifierSuper})
	}
	assert.Empty(t, in.String())
}

// TestPaste_BracketedWithBothMarkers: GW8's third suspect. A paste of several lines goes in one write
// with the start and the end marker of a bracketed paste, a held ⇧ or not (it always did).
func TestPaste_BracketedWithBothMarkers(t *testing.T) {
	saved := goos
	goos = "darwin"
	defer func() { goos = saved }()
	app := test.NewTempApp(t)
	app.Clipboard().SetContent("kubectl get ns\nkubectl get pods -A | head -3\nkubectl get nodes")
	in := &bytes.Buffer{}
	term := &Terminal{in: NopCloser(in), bracketedPasteMode: true}
	term.setupShortcuts()
	term.KeyDown(&fyne.KeyEvent{Name: desktop.KeyShiftLeft})
	term.TypedShortcut(&fyne.ShortcutPaste{})
	assert.Equal(t, "\x1b[200~kubectl get ns\nkubectl get pods -A | head -3\nkubectl get nodes\x1b[201~", in.String())
}

// TestDragged_BlockFromThePress: a drag selects a block when ⌥ (Alt) was held at its press, as the
// press's own event says. Cogpit patch: the widget read ⌥ from the keys it saw go down and up, so an
// ⌥ whose release it never saw (GW8's way) made every later drag a block, and ⌥ held at the press
// counted only if its KeyDown had reached the terminal.
func TestDragged_BlockFromThePress(t *testing.T) {
	test.NewTempApp(t)
	newTerm := func() *Terminal {
		grid := widget2.NewTermGrid()
		grid.SetText("Hello World!\nTesting 123.\nThird row...")
		term := &Terminal{content: grid}
		term.Resize(fyne.NewSize(500, 500))
		return term
	}
	drag := func(term *Terminal) {
		from, to := term.getTextPosition(position{Row: 1, Col: 1}), term.getTextPosition(position{Row: 2, Col: 5})
		term.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: to}, Dragged: fyne.NewDelta(to.X-from.X, to.Y-from.Y)})
		term.DragEnd()
	}

	term := newTerm()
	term.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary, Modifier: fyne.KeyModifierAlt})
	drag(term)
	assert.Equal(t, "Hello\nTesti", term.SelectedText(), "⌥ at the press")

	term = newTerm()
	term.KeyDown(&fyne.KeyEvent{Name: desktop.KeyAltLeft}) // and never up
	term.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary})
	drag(term)
	assert.Equal(t, "Hello World!\nTesti", term.SelectedText(), "⌥ seen going down long before")
}
