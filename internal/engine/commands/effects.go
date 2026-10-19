package commands

// The names of the confirmation rows (engine.Effect) that several commands give, so that the same
// fact reads the same everywhere (D-100).
const (
	effectArchive = "Archive" // how a chart's archive is checked: Helm install and upgrade
	effectValues  = "Values"  // where a chart's values come from; a Secret's, that none is shown
)

// updateStrategyOnDelete is the update strategy whose pods a restart replaces only as they are
// deleted: its confirmation says so.
const updateStrategyOnDelete = "OnDelete"
