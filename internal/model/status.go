package model

// Status is a status token. Rows and cells carry a token; the UI maps it to a theme colour, so
// no view decides colours itself.
type Status string

// Status tokens. Keep the set small: every token needs a colour in every theme.
const (
	StatusNone        Status = ""            // no status colour
	StatusOK          Status = "ok"          // Running, Ready, Active, Available, Normal events
	StatusSucceeded   Status = "succeeded"   // Completed, Succeeded
	StatusPending     Status = "pending"     // Pending, ContainerCreating, progressing
	StatusWarning     Status = "warning"     // Warning events, partly ready, pressure conditions
	StatusError       Status = "error"       // CrashLoopBackOff, Failed, Error, NotReady
	StatusTerminating Status = "terminating" // deletionTimestamp set
	StatusUnknown     Status = "unknown"     // state could not be determined
	StatusStopped     Status = "stopped"     // a container that ran and stopped (a finished init container)
	// StatusEphemeral is a kind of container rather than a state: an ephemeral (debug) container's
	// square in a pod's Containers cell, in the palette's blue (D-102); the square's fill says its
	// state (CellMark.Hollow once it has finished).
	StatusEphemeral Status = "ephemeral"
)
