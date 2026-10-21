package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
)

// The two preference files of this area (preferences.go, clustersettings.go) are kept as the
// hotbar's is: one JSON document under Cogpit's configuration directory, read once, written whole
// through writePrivateFile (0600 in a 0700 directory, replaced atomically), never a kubeconfig. A
// file that does not load leaves the defaults and is moved to <file>.unreadable before the first
// change writes, so that a change never silently overwrites what the user may want back.

// readPrefsDoc decodes the file at path into doc. A missing file leaves doc alone and is no error;
// a file that is larger than maxPrivateDoc, not a regular file, or not JSON is.
func readPrefsDoc(path string, doc any) error {
	if path == "" {
		return nil
	}
	return readPrivateJSON(path, maxPrivateDoc, doc)
}

// writePrefsDoc writes doc to path, owner-only and atomically; when unreadable is set, the file
// that did not load is first moved aside to <path>.unreadable and unreadable cleared.
func writePrefsDoc(path string, unreadable *bool, doc any) error {
	if path == "" {
		return nil
	}
	if *unreadable {
		if err := renameFile(path, path+".unreadable"); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("keeping the unreadable file aside: %w", err)
		}
		*unreadable = false
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(path, append(data, '\n'))
}
