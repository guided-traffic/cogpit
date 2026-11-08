package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// CopyResult is object.copyName's result: the object's name, which the GUI puts on the clipboard.
type CopyResult struct {
	Ref  model.ObjectRef `json:"ref"`
	Name string          `json:"name"`
}

// orderCopy puts Copy first among the drawer's actions, before every kind's own (Lens's drawer:
// Copy, then the kind's actions, Edit and Delete).
const orderCopy = 10

func registerCopy(g *registrar) {
	add(g, engine.Spec[refParams]{
		ID: "object.copyName", Label: "Copy", MenuPath: "Object/Copy Name", Order: orderCopy, DrawerOnly: true, RowKeys: []string{"c"},
		Danger: engine.DangerUI,
		Description: "Returns an object's name, as the detail drawer's Copy does for the user, who gets it on the " +
			"clipboard. Works for every kind; the object must be in Cogpit's cache.",
		Enabled: func(s *engine.Session, p refParams) error {
			if _, err := connectedOn(s, p.Ref.Context); err != nil {
				return err
			}
			_, _, err := s.ResolveRef(p.Ref)
			return err
		},
		Run: func(ctx context.Context, s *engine.Session, p refParams) (any, error) {
			ref, _, err := s.ResolveRef(p.Ref)
			if err != nil {
				return nil, err
			}
			obj, err := s.Object(ctx, ref)
			if err != nil {
				return nil, err
			}
			return CopyResult{Ref: ref, Name: obj.GetName()}, nil
		},
	})
}
