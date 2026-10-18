package commands

import "time"

// SetFluxJobWait sets how long Reconcile with Source waits for each controller (fluxJobWait) and
// returns what restores it. Tests set it before their session starts jobs and restore it after the
// session closed.
func SetFluxJobWait(d time.Duration) (restore func()) {
	old := fluxJobWait
	fluxJobWait = d
	return func() { fluxJobWait = old }
}

// CSVFilename is csvFilename, the file name table.exportCsv suggests.
func CSVFilename(view, cluster string) string { return csvFilename(view, cluster) }
