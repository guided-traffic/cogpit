package model

// KindNames are the names a cluster gives a kind besides its plural, as kubectl and k9s accept
// them: its singular name and its short names ("po", "deploy"). A built-in kind's come from the
// cluster's API discovery, a custom kind's from its definition (spec.names); the palette's jump
// matches them (engine/jump.go).
type KindNames struct {
	Singular   string   `json:"singular,omitempty"`
	ShortNames []string `json:"shortNames,omitempty"`
}
