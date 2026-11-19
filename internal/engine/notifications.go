package engine

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"sigs.k8s.io/yaml"
)

// The notification log (G5, ui-parity › Appearance: "Notifications in the title bar"): every toast
// the user was shown, newest last, so that the title bar's bell can list what the user may have
// missed and agents can read what the user was told (notifications.list). It is the session's, in
// memory: a restart starts empty (Cogpit's default, not the owner's decision).
//
// What a notification carries is what its toast carried: a level and a message, never a value
// (architecture §4.3). Two kinds of message are the window's (Notification.WindowOnly):
//
//   - the window's own toasts (RecordWindowToast): a GUI command's error, which the engine passed
//     to the window in full because the window is the user's (a failed Helm install's error, H-31),
//     and the window's own messages;
//   - an engine toast marked Event.WindowOnly (EmitWindowToast): one that quotes what the cluster
//     returned to a Helm action, which a chart's templates can fill from a Secret (H-31).
//
// A caller outside the window (an agent, the CLI) reads them as text (the owner's A19): the inline Helm values the action was given removed when the toast
// was recorded, and the cached Secrets' values replaced when it is read (AgentView, H-82). Marking
// notifications read and clearing them are the window's (A18).
//
// The log is held under Session.mu with the UI state, so its summary in UIState.Notifications
// never disagrees with it. Changing it bumps the UI state's revision and emits EventUIState without
// going through UpdateUI: nothing a view shows depends on it, and toasts are emitted from view
// workers and recover handlers that must not wait for the views to follow a change.

// ErrNotificationsGUIOnly refuses marking notifications read or clearing them from anywhere but the
// window (A18, owner): what the user has not seen yet stays unread for the user.
var ErrNotificationsGUIOnly = errors.New("only the user, in Cogpit's own window, marks notifications read or clears them; " +
	"agents read the list (notifications_list)")

// MaxNotifications is how many notifications the log keeps; older ones are dropped.
const MaxNotifications = 100

// Notification is one toast the user was shown.
type Notification struct {
	// ID numbers the session's notifications from 1, in the order they were shown.
	ID uint64 `json:"id"`
	// Level is LevelInfo, LevelWarning or LevelError.
	Level string `json:"level"`
	// Message is the toast's text; a caller outside the window reads a WindowOnly one filtered (A19).
	Message string    `json:"message"`
	Time    time.Time `json:"time"`
	// Read is set once the user saw it in the notification list or closed its toast.
	Read bool `json:"read,omitempty"`
	// WindowOnly: the message is the window's; a caller outside it reads agentText, filtered (see
	// above).
	WindowOnly bool `json:"windowOnly,omitempty"`
	// agentText is a WindowOnly message as callers outside the window read it before the Secret
	// filter: the inline Helm values the action was given removed (EmitWindowToast).
	agentText string
}

// NotificationSummary is what UIState.Notifications reports of the log: enough for the bell's
// badge, and a change of any field tells the window to read the list again.
type NotificationSummary struct {
	Total  int `json:"total"`
	Unread int `json:"unread"`
	// UnreadLevel is the most severe level among the unread ones, "" when all are read.
	UnreadLevel string `json:"unreadLevel,omitempty"`
	// Latest is the newest notification's id, 0 for an empty log.
	Latest uint64 `json:"latest,omitempty"`
}

// NotificationList is the result of notifications.list: the notifications newest first.
type NotificationList struct {
	Notifications []Notification `json:"notifications"`
	NotificationSummary
}

// notificationLog is the bounded log, oldest first. Guarded by Session.mu.
type notificationLog struct {
	items []Notification
	seq   uint64
}

func (l *notificationLog) add(n Notification) uint64 {
	l.seq++
	n.ID = l.seq
	l.items = append(l.items, n)
	if over := len(l.items) - MaxNotifications; over > 0 {
		l.items = slices.Delete(l.items, 0, over)
	}
	return n.ID
}

func (l *notificationLog) summary() NotificationSummary {
	var s NotificationSummary
	s.Total = len(l.items)
	if s.Total > 0 {
		s.Latest = l.items[s.Total-1].ID
	}
	for _, n := range l.items {
		if !n.Read {
			s.Unread++
			if levelRank(n.Level) > levelRank(s.UnreadLevel) {
				s.UnreadLevel = n.Level
			}
		}
	}
	return s
}

// levelRank orders the levels by severity; "" (none) is the lowest.
func levelRank(level string) int {
	switch level {
	case LevelError:
		return 3
	case LevelWarning:
		return 2
	case LevelInfo:
		return 1
	}
	return 0
}

// toastLevel is the level a toast is shown at: an unknown level is an info, as the window shows it.
func toastLevel(level string) string {
	if levelRank(level) == 0 {
		return LevelInfo
	}
	return level
}

// changeNotifications changes the log with fn under the session's lock and, if fn reports a
// change, publishes the new summary as a new UI state revision. It returns the UI state.
func (s *Session) changeNotifications(fn func(*notificationLog) bool) UIState {
	s.mu.Lock()
	if !fn(&s.notes) {
		now := s.ui.Clone()
		s.mu.Unlock()
		return now
	}
	s.ui.Notifications = s.notes.summary()
	s.ui.Revision++
	now := s.ui.Clone()
	s.mu.Unlock()
	s.emit(Event{Type: EventUIState, UIState: &now})
	return now
}

// recordToast adds an engine toast to the log and returns its id. emit calls it for every
// EventToast before the subscribers see the toast.
func (s *Session) recordToast(level, message string, windowOnly bool, agentText string) uint64 {
	if message == "" {
		return 0
	}
	if agentText == "" {
		agentText = message
	}
	var id uint64
	s.changeNotifications(func(l *notificationLog) bool {
		id = l.add(Notification{Level: toastLevel(level), Message: message, Time: s.opts.Now(), WindowOnly: windowOnly, agentText: agentText})
		return true
	})
	return id
}

// EmitWindowToast emits a toast that quotes what the cluster returned to a Helm action (H-31): the
// window shows it whole; callers outside it read it with inline, the values the action was given
// inline, removed, and the cached Secrets' values replaced (A19).
func (s *Session) EmitWindowToast(level, message, inline string) {
	s.emit(Event{Type: EventToast, Level: level, Message: message, WindowOnly: true, agentText: withoutInlineValues(message, inline)})
}

// inlineValueMark stands for an inline Helm value removed from a notification for agents.
const inlineValueMark = "«inline value»"

// minInlineValue is the shortest inline value removed: shorter ones ("1", "true", "web") would
// cut ordinary words out of the message.
const minInlineValue = 4

// withoutInlineValues returns message with each of inline's scalar values (of at least
// minInlineValue bytes) replaced by inlineValueMark, longest first; inline that does not parse
// as YAML is taken line by line, each line's text after its first ": ".
func withoutInlineValues(message, inline string) string {
	if strings.TrimSpace(inline) == "" {
		return message
	}
	var values []string
	var doc any
	if err := yaml.Unmarshal([]byte(inline), &doc); err == nil {
		values = scalarStrings(doc, values)
	} else {
		for _, line := range strings.Split(inline, "\n") {
			if _, v, ok := strings.Cut(line, ": "); ok {
				values = append(values, strings.TrimSpace(v))
			} else {
				values = append(values, strings.TrimSpace(line))
			}
		}
	}
	values = append(values, strings.TrimSpace(inline))
	slices.SortFunc(values, func(a, b string) int { return len(b) - len(a) })
	for _, v := range slices.Compact(values) {
		if len(v) >= minInlineValue {
			message = strings.ReplaceAll(message, v, inlineValueMark)
		}
	}
	return message
}

// scalarStrings appends the text of every scalar in a decoded YAML document.
func scalarStrings(v any, out []string) []string {
	switch x := v.(type) {
	case map[string]any:
		for _, e := range x {
			out = scalarStrings(e, out)
		}
	case []any:
		for _, e := range x {
			out = scalarStrings(e, out)
		}
	case nil:
	default:
		out = append(out, fmt.Sprint(x))
	}
	return out
}

// RecordWindowToast adds a toast the window raised itself (a GUI command's error, a copy
// confirmation) to the log, its message for the window only, and returns its id. The window
// calls it for every toast that did not come from an engine event.
func (s *Session) RecordWindowToast(level, message string) uint64 {
	return s.recordToast(level, message, true, "")
}

// Notifications returns the log newest first. For a caller outside the window (window false) the
// message of a WindowOnly notification is its agentText with the cached Secrets' values replaced
// (A19).
func (s *Session) Notifications(window bool) NotificationList {
	s.mu.Lock()
	out := NotificationList{Notifications: make([]Notification, 0, len(s.notes.items)), NotificationSummary: s.notes.summary()}
	for i := len(s.notes.items) - 1; i >= 0; i-- {
		out.Notifications = append(out.Notifications, s.notes.items[i])
	}
	s.mu.Unlock()
	if window {
		return out
	}
	var view *AgentView // the backend's matcher, outside s.mu: agentView takes it
	for i := range out.Notifications {
		if n := &out.Notifications[i]; n.WindowOnly {
			if view == nil {
				view = s.agentView()
			}
			n.Message = view.Text(n.agentText)
		}
	}
	return out
}

// MarkNotificationsRead marks the notifications with the given ids read, every one when ids is
// empty, and returns the UI state. Ids the log no longer holds are ignored.
func (s *Session) MarkNotificationsRead(ids []uint64) UIState {
	return s.changeNotifications(func(l *notificationLog) bool {
		changed := false
		for i := range l.items {
			if !l.items[i].Read && (len(ids) == 0 || slices.Contains(ids, l.items[i].ID)) {
				l.items[i].Read = true
				changed = true
			}
		}
		return changed
	})
}

// ClearNotifications empties the log and returns the UI state. Ids go on from where they were.
func (s *Session) ClearNotifications() UIState {
	return s.changeNotifications(func(l *notificationLog) bool {
		if len(l.items) == 0 {
			return false
		}
		l.items = nil
		return true
	})
}
