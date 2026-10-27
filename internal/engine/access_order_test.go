package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A command that does not apply stays hidden although the access reviews deny it, so a cordoned
// node offers Uncordon greyed, not Cordon and Uncordon both; an applicable command is greyed by the
// denial before its own reason. A verb on "*" is not asked for a virtual kind: Edit and Delete do
// not apply to it, and "*" names no API resource there.
func TestAccessDenialAfterNotApplicable(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, testkit.Pod("apps", "web"))
	s, fc := newTestSession(t, src, 5*time.Millisecond)
	fc.profiles = append(testProfiles(), releasesKind)
	run := func(context.Context, *Session, accessParams) (any, error) { return "ran", nil }
	for _, spec := range []Spec[accessParams]{
		{ID: "test.cordon", Danger: DangerRead, Verbs: []Verb{{Verb: "patch", Resource: "pods"}}, Run: run,
			Enabled: func(*Session, accessParams) error { return fmt.Errorf("already cordoned: %w", ErrNotApplicable) }},
		{ID: "test.busy", Danger: DangerRead, Verbs: []Verb{{Verb: "patch", Resource: "pods"}}, Run: run,
			Enabled: func(*Session, accessParams) error { return errors.New("busy") }},
		{ID: "test.del", Danger: DangerRead, Verbs: []Verb{{Verb: "delete", Resource: "*"}}, Run: run},
	} {
		if err := Register(s.Registry(), spec); err != nil {
			t.Fatal(err)
		}
	}
	fc.access = fakeAccess{deny: map[string]bool{
		"patch pods/ apps":   true,
		"delete secrets/ ns": true,
	}}
	connectTest(t, s)
	enabled := func(id string, ref model.ObjectRef) error {
		params, _ := json.Marshal(accessParams{Ref: ref})
		cmd, _ := s.Registry().Get(id)
		return cmd.Enabled(s, params)
	}
	pod := model.ObjectRef{Kind: "pods", Namespace: "apps", Name: "web"}
	if err := enabled("test.cordon", pod); !errors.Is(err, ErrNotApplicable) || errors.Is(err, ErrNotPermitted) {
		t.Errorf("not applicable and denied: %v, want ErrNotApplicable", err)
	}
	if err := enabled("test.busy", pod); !errors.Is(err, ErrNotPermitted) {
		t.Errorf("applicable, busy and denied: %v, want ErrNotPermitted", err)
	}
	if err := enabled("test.del", model.ObjectRef{Kind: releasesKind.Key, Namespace: "ns", Name: "web"}); err != nil {
		t.Errorf("delete on a virtual kind was asked as delete secrets: %v", err)
	}
}
