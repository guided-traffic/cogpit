package engine

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// The connection log on disk (D-66; the owner's decision of 2026-10-16): one file per context in
// Cogpit's configuration directory, <user config dir>/Cogpit/connection-logs (app.Dirs.ConnectLogDir),
// owner-only like the audit log (the directory 0700, the files 0600). A file is named by the
// SHA-256 of the context id (its first 32 hex digits and ".jsonl"), never by the id itself, which
// may hold any character; the id is stored inside, in each attempt's first record, and an attempt
// whose record names another context is not read.
//
// A file is JSON lines, one record each, appended as it happens (O_APPEND, one write per record),
// so a restart or a force-quit keeps every line written before it; a last line a crash cut short
// does not decode and is skipped. An attempt's records: "begin" (its kind and context), its lines,
// and "end" (its outcome and duration); one without an end ended when Cogpit stopped.
//
// Rotation: an attempt whose last record is older than connectLogRetention (7 days) is dropped,
// and the oldest attempts go until the file holds at most connectLogMaxBytes (1 MiB) less the room
// one attempt may take (connectLogAttemptRoom); the newest attempt always stays. A file is
// compacted when an attempt begins and it is larger than that, and at the first attempt of a run
// (when it may hold attempts past the retention); files of contexts left alone for longer than the
// retention are deleted at the first write of a run (sweep). A compaction replaces the file
// atomically (writePrivateFile); a record another Cogpit process appends at that moment can be lost.

const (
	// ConnectLogDirName is the connection log's directory under Cogpit's configuration directory.
	ConnectLogDirName = "connection-logs"
	// connectLogRetention is how long an attempt is kept.
	connectLogRetention = 7 * 24 * time.Hour
	// connectLogMaxBytes is the most a context's file holds.
	connectLogMaxBytes = 1 << 20
	// connectLogAttemptRoom is what one attempt may add to a file: maxAttemptLines lines of at most
	// maxConnectLogText bytes, as JSON, with room to spare.
	connectLogAttemptRoom = 256 << 10
	// connectLogReadLimit bounds a read: a file several processes grew past the limit is read from
	// its end.
	connectLogReadLimit = 4 * connectLogMaxBytes
	// connectLogSuffix is the files' suffix.
	connectLogSuffix = ".jsonl"
)

// Record events.
const (
	recordBegin = "begin"
	recordEnd   = "end"
	recordStage = "stage" // a connect's stage: its name in step, its result in out (D-114)
)

// logRecord is one line of a connection log file.
type logRecord struct {
	Time    time.Time `json:"t"`
	Context string    `json:"ctx,omitempty"` // on begin: the context id
	Attempt int64     `json:"a"`
	Event   string    `json:"ev,omitempty"`   // recordBegin, recordEnd, or empty for a line
	Kind    string    `json:"kind,omitempty"` // on begin
	Outcome string    `json:"out,omitempty"`  // on end
	Level   string    `json:"lv,omitempty"`
	Text    string    `json:"msg,omitempty"`
	Step    string    `json:"step,omitempty"`
	TookMS  int64     `json:"ms,omitempty"`
}

// connectLogFiles are the connection log's files in dir.
type connectLogFiles struct {
	dir string

	mu        sync.Mutex
	checked   map[string]bool // contexts whose file was compacted in this run
	dirOK     bool            // the directory was made and checked in this run
	swept     bool
	sweepWait sync.WaitGroup // the sweep (tests wait for it)
}

// newConnectLogFiles keeps the log in dir; nil for "".
func newConnectLogFiles(dir string) *connectLogFiles {
	if dir == "" {
		return nil
	}
	return &connectLogFiles{dir: dir, checked: map[string]bool{}}
}

// ConnectLogFileName is the file name of a context's connection log: the first 32 hex digits of the
// SHA-256 of its id, and ".jsonl".
func ConnectLogFileName(contextID string) string {
	sum := sha256.Sum256([]byte(contextID))
	return hex.EncodeToString(sum[:16]) + connectLogSuffix
}

func (f *connectLogFiles) path(ctx string) string {
	return filepath.Join(f.dir, ConnectLogFileName(ctx))
}

// append writes rec to ctx's file; begins says an attempt begins, when the file is compacted if it
// needs to be.
func (f *connectLogFiles) append(ctx string, rec logRecord, begins bool) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.dirOK || begins {
		if err := ensurePrivateDir(f.dir); err != nil {
			f.dirOK = false
			return fmt.Errorf("connection log: %w", err)
		}
		f.dirOK = true
	}
	if !f.swept {
		f.swept = true
		f.sweepWait.Add(1)
		go f.sweep(time.Now()) // the files' times are the computer's, not the session's clock
	}
	path := f.path(ctx)
	if begins {
		if err := f.compactLocked(ctx, path, rec.Time); err != nil {
			return fmt.Errorf("connection log: %w", err)
		}
	}
	err = appendPrivate(path, line)
	if err != nil {
		f.dirOK = false // made again at the next write
	}
	return err
}

// appendPrivate appends data to the owner-only file at path, creating it 0600; a symlink or other
// non-regular file is refused.
func appendPrivate(path string, data []byte) error {
	switch fi, err := os.Lstat(path); {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return fmt.Errorf("connection log: %w", err)
	case !fi.Mode().IsRegular():
		return fmt.Errorf("connection log: %s is not a regular file", path)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("connection log: %w", err)
	}
	_, werr := file.Write(data)
	cerr := file.Close()
	if werr != nil {
		return fmt.Errorf("connection log: %w", werr)
	}
	if cerr != nil {
		return fmt.Errorf("connection log: %w", cerr)
	}
	return nil
}

// compactLocked rewrites ctx's file without what the rotation drops, when it is large or not yet
// looked at in this run.
func (f *connectLogFiles) compactLocked(ctx, path string, now time.Time) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		f.checked[ctx] = true
		return nil
	}
	if err != nil {
		return err
	}
	if f.checked[ctx] && fi.Size() <= connectLogMaxBytes-connectLogAttemptRoom {
		return nil
	}
	f.checked[ctx] = true
	data, err := readLogFile(path)
	if err != nil {
		return err
	}
	kept := rotate(data, now)
	if bytes.Equal(kept, data) {
		return nil
	}
	return writePrivateFile(path, kept)
}

// rotate returns a file's records without the attempts past the retention and, oldest first,
// without the attempts beyond the size the file may keep before an attempt begins; the newest
// attempt stays. Records that do not decode, and lines of an attempt whose begin is gone, are
// dropped.
func rotate(data []byte, now time.Time) []byte {
	type group struct {
		id    int64
		lines [][]byte
		size  int
		last  time.Time
	}
	var groups []*group
	byID := map[int64]*group{}
	for _, raw := range bytes.Split(data, []byte("\n")) {
		var rec logRecord
		if len(bytes.TrimSpace(raw)) == 0 || json.Unmarshal(raw, &rec) != nil {
			continue
		}
		g := byID[rec.Attempt]
		if g == nil {
			if rec.Event != recordBegin {
				continue // its begin was dropped
			}
			g = &group{id: rec.Attempt}
			byID[rec.Attempt] = g
			groups = append(groups, g)
		}
		g.lines = append(g.lines, raw)
		g.size += len(raw) + 1
		if rec.Time.After(g.last) {
			g.last = rec.Time
		}
	}
	groups = slices.DeleteFunc(groups, func(g *group) bool { return now.Sub(g.last) > connectLogRetention })
	total := 0
	for _, g := range groups {
		total += g.size
	}
	for len(groups) > 1 && total > connectLogMaxBytes-connectLogAttemptRoom {
		total -= groups[0].size
		groups = groups[1:]
	}
	var out bytes.Buffer
	for _, g := range groups {
		for _, l := range g.lines {
			out.Write(l)
			out.WriteByte('\n')
		}
	}
	return out.Bytes()
}

// readLogFile reads a connection log file, at most connectLogReadLimit bytes from its end; a
// symlink or other non-regular file is refused.
func readLogFile(path string) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	if fi.Size() > connectLogReadLimit {
		if _, err := file.Seek(fi.Size()-connectLogReadLimit, io.SeekStart); err != nil {
			return nil, err
		}
	}
	return io.ReadAll(io.LimitReader(file, connectLogReadLimit))
}

// load reads ctx's attempts from its file: those whose begin names ctx, oldest first. A missing file
// is no error. It takes no lock, so a read never holds up a connect's writes: a line is one append,
// and a compaction replaces the file by a rename, so the read sees the old file or the new one.
func (f *connectLogFiles) load(ctx string, now time.Time) ([]*ConnectAttempt, error) {
	data, err := readLogFile(f.path(ctx))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("connection log: %w", err)
	}
	return parseAttempts(data, ctx, now), nil
}

// parseAttempts builds the attempts of ctx from a file's records; a record that does not decode (a
// line a crash cut short) is skipped.
func parseAttempts(data []byte, ctx string, now time.Time) []*ConnectAttempt {
	var out []*ConnectAttempt
	byID := map[int64]*ConnectAttempt{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		var rec logRecord
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		a := byID[rec.Attempt]
		switch {
		case rec.Event == recordBegin:
			if rec.Context != ctx || a != nil {
				continue
			}
			a = &ConnectAttempt{Kind: rec.Kind, Context: ctx, Start: rec.Time, Lines: []ConnectLogLine{}, id: rec.Attempt}
			byID[rec.Attempt] = a
			out = append(out, a)
			continue
		case a == nil:
			continue
		case rec.Event == recordEnd:
			a.End, a.Outcome, a.TookMS = rec.Time, rec.Outcome, rec.TookMS
			continue
		case rec.Event == recordStage:
			if rec.Step != "" {
				a.addStage(ConnectStage{Name: rec.Step, Result: rec.Outcome, Text: rec.Text, TookMS: rec.TookMS})
			}
			continue
		}
		if rec.Text == "" {
			continue
		}
		if len(a.Lines) >= maxAttemptLines {
			a.Omitted++
			continue
		}
		a.Lines = append(a.Lines, ConnectLogLine{Time: rec.Time, Level: rec.Level, Text: rec.Text, Step: rec.Step, TookMS: rec.TookMS})
	}
	return slices.DeleteFunc(out, func(a *ConnectAttempt) bool { return now.Sub(a.last()) > connectLogRetention })
}

// sweep deletes the files of contexts nobody connected to within the retention (their last write
// is older), and temporary files a crash left. It runs once per run, on its own goroutine.
func (f *connectLogFiles) sweep(now time.Time) {
	defer f.sweepWait.Done()
	defer func() { _ = recover() }() // a sweep that fails leaves the files; the next run tries again
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		ours := strings.HasSuffix(name, connectLogSuffix) || strings.HasSuffix(name, ".tmp")
		if !e.Type().IsRegular() || !ours {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) <= connectLogRetention {
			continue
		}
		_ = os.Remove(filepath.Join(f.dir, name))
	}
}
