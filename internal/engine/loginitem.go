package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Start at login (D-48, the lead's decision for the owner to confirm; ui-parity › Preferences ›
// Start at login; Lens's Preferences › Application › "Automatic start-up", inferred from its
// documentation). Whether Cogpit starts when the user logs in is the operating system's state, not
// Cogpit's file: on macOS 13 and later the app's own login item (SMAppService.mainAppService),
// which the user also sees and switches in System Settings › General › Login Items. So Cogpit keeps
// no copy: UIState.LoginItem shows the status as macOS reports it, read when the Preferences ask
// (preferences.loginItem) and after every change, and the window's check follows it. The desktop
// binary wires the operating system's side in (Options.LoginItem); the engine makes no such call
// itself. Without one (Windows, Linux, cogpit-cli) UIState.LoginItem is nil and the row is not
// offered. Off by default: nothing is registered until the user turns it on.
//
// Only the user turns it on, in Cogpit's window (ErrStartAtLoginGUIOnly): it makes Cogpit run
// without being opened, its MCP server included once the user switched that on (it is opt-in, D-49).
// Agents and the CLI may turn it off, as they may turn off chart icons (D-46).

// LoginItem is the operating system's start at login for Cogpit's app. Implementations need not
// be safe for concurrent use: the session serialises its calls.
type LoginItem interface {
	// Status reads the login item's status: LoginItemOff, LoginItemOn, LoginItemNeedsApproval or
	// LoginItemNotFound.
	Status() string
	// Register makes the system start Cogpit at login; Unregister stops it.
	Register() error
	Unregister() error
	// OpenSettings opens the system's list of login items (System Settings › General › Login Items).
	OpenSettings() error
}

// The statuses of the login item, as macOS's SMAppService reports them.
const (
	// LoginItemUnknown: not read yet.
	LoginItemUnknown = ""
	// LoginItemOff: not registered (SMAppServiceStatusNotRegistered); the default.
	LoginItemOff = "off"
	// LoginItemOn: registered and allowed (SMAppServiceStatusEnabled).
	LoginItemOn = "on"
	// LoginItemNeedsApproval: registered, and macOS waits for the user to allow it in System
	// Settings › General › Login Items (SMAppServiceStatusRequiresApproval).
	LoginItemNeedsApproval = "requiresApproval"
	// LoginItemNotFound: macOS finds no login item for this app (SMAppServiceStatusNotFound): not
	// a signed Cogpit.app it knows, such as a development build run from the repository.
	LoginItemNotFound = "notFound"
)

// LoginItemState is UIState.LoginItem: the status as the operating system last reported it.
type LoginItemState struct {
	Status string `json:"status"`
}

// Errors of start at login.
var (
	// ErrNoLoginItem disables the commands where the platform offers no start at login.
	ErrNoLoginItem = errors.New("this Cogpit offers no start at login: only the macOS app does (macOS 13 or later)")
	// ErrStartAtLoginGUIOnly refuses turning start at login on from anywhere but the user's own
	// action in Cogpit's window.
	ErrStartAtLoginGUIOnly = errors.New("only the user, in Cogpit's own window, can turn on start at login; agents and the " +
		"CLI may only turn it off (it makes Cogpit run without being opened, its MCP server included if the user switched that on)")
	// ErrLoginItemsGUIOnly refuses opening System Settings' login items to every origin but the GUI.
	ErrLoginItemsGUIOnly = errors.New("only the user, in Cogpit's own window, can open System Settings › General › Login Items")
)

// serialLoginItem serialises the calls to the operating system's login item.
type serialLoginItem struct {
	mu    sync.Mutex
	inner LoginItem
}

func (l *serialLoginItem) Status() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return checkLoginItemStatus(l.inner.Status())
}

func (l *serialLoginItem) Register() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.inner.Register()
}

func (l *serialLoginItem) Unregister() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.inner.Unregister()
}

func (l *serialLoginItem) OpenSettings() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.inner.OpenSettings()
}

// checkLoginItemStatus maps a status the platform does not name to LoginItemNotFound.
func checkLoginItemStatus(st string) string {
	switch st {
	case LoginItemOff, LoginItemOn, LoginItemNeedsApproval, LoginItemNotFound:
		return st
	}
	return LoginItemNotFound
}

// attachLoginItem offers start at login when the binary wired the operating system's in; its
// status is read when first asked (RefreshLoginItem), not at start.
func (s *Session) attachLoginItem() {
	if s.opts.LoginItem == nil {
		return
	}
	s.opts.LoginItem = &serialLoginItem{inner: s.opts.LoginItem}
	s.ui.LoginItem = &LoginItemState{}
}

// HasLoginItem reports whether the session offers start at login.
func (s *Session) HasLoginItem() bool { return s.opts.LoginItem != nil }

// RefreshLoginItem reads the login item's status from the operating system and shows it
// (UIState.LoginItem).
func (s *Session) RefreshLoginItem() (LoginItemState, error) {
	li := s.opts.LoginItem
	if li == nil {
		return LoginItemState{}, ErrNoLoginItem
	}
	st := LoginItemState{Status: li.Status()}
	s.UpdateUI(func(u *UIState) { u.LoginItem = &st })
	return st, nil
}

// RefuseStartAtLogin refuses turning start at login on from another origin than the GUI.
func RefuseStartAtLogin(ctx context.Context, on bool) error {
	if on && OriginOf(ctx) != OriginGUI {
		return ErrStartAtLoginGUIOnly
	}
	return nil
}

// SetStartAtLogin registers Cogpit's login item or removes it, then reads its status and shows it.
// A registration that leaves the item on or waiting for the user's approval succeeded, whatever
// the call said (registering twice is no failure); so did a removal that leaves it off or not
// found. Otherwise the call's error is returned, with the status shown as it is.
func (s *Session) SetStartAtLogin(ctx context.Context, on bool) (LoginItemState, error) {
	li := s.opts.LoginItem
	if li == nil {
		return LoginItemState{}, ErrNoLoginItem
	}
	if err := RefuseStartAtLogin(ctx, on); err != nil {
		return LoginItemState{}, err
	}
	var err error
	if on {
		err = li.Register()
	} else {
		err = li.Unregister()
	}
	st, _ := s.RefreshLoginItem()
	switch {
	case on && (st.Status == LoginItemOn || st.Status == LoginItemNeedsApproval):
		return st, nil
	case !on && (st.Status == LoginItemOff || st.Status == LoginItemNotFound):
		return st, nil
	case err == nil && on:
		err = fmt.Errorf("macOS reports the login item as %s after registering it", st.Status)
	case err == nil:
		err = fmt.Errorf("macOS reports the login item as %s after removing it", st.Status)
	}
	if on {
		return st, fmt.Errorf("start at login: %w", err)
	}
	return st, fmt.Errorf("stop starting at login: %w", err)
}

// OpenLoginItemsSettings opens System Settings › General › Login Items, where the user allows a
// login item that waits for approval.
func (s *Session) OpenLoginItemsSettings() error {
	li := s.opts.LoginItem
	if li == nil {
		return ErrNoLoginItem
	}
	return li.OpenSettings()
}
