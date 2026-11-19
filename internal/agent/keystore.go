package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"runtime"
	"sync"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The API keys of the LLM connections live in a file of their own in Cogpit's configuration
// directory (KeysFile), owner-only, never in the connections file and never in the operating
// system's keychain: the owner's decision (D-10), since every self-built binary carries another
// code signature and macOS asked for the login password again and again. Cogpit neither reads nor
// removes the keychain item older versions wrote ("Cogpit LLM connection"): reading it would
// ask that very question. A connection whose key was there shows "no API key" until the user enters
// the key once more (Agent.checkKeys).

// ErrNoKey is returned by KeyStore.Get when no key is stored for the account.
var ErrNoKey = errors.New("no API key stored for this connection")

// errKeysFileInvalid is the error of a key file that does not decode. It names the file only: a
// JSON decoder's message can quote the file's bytes, which hold keys.
var errKeysFileInvalid = errors.New("it is not a key file Cogpit wrote")

// KeyStore keeps the API keys of the LLM connections apart from the connections: the owner-only
// key file (FileKeyStore, the desktop binary's), or memory (MemoryKeyStore: tests, and a Cogpit
// without a configuration directory). Errors never carry a key.
type KeyStore interface {
	// Get returns the key stored for account, ErrNoKey when there is none.
	Get(account string) (string, error)
	// Set stores key for account, replacing one stored before.
	Set(account, key string) error
	// Delete removes the key of account; nil when there was none.
	Delete(account string) error
}

// MemoryKeyStore is a KeyStore in memory: for tests, and for a Cogpit without a configuration
// directory, whose connections last for the session too.
type MemoryKeyStore struct {
	mu   sync.Mutex
	keys map[string]string
}

// NewMemoryKeyStore returns an empty MemoryKeyStore.
func NewMemoryKeyStore() *MemoryKeyStore { return &MemoryKeyStore{keys: map[string]string{}} }

// Get implements KeyStore.
func (m *MemoryKeyStore) Get(account string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keys[account]
	if !ok {
		return "", ErrNoKey
	}
	return k, nil
}

// Set implements KeyStore.
func (m *MemoryKeyStore) Set(account, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys[account] = key
	return nil
}

// Delete implements KeyStore.
func (m *MemoryKeyStore) Delete(account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.keys, account)
	return nil
}

// Len returns how many keys are stored.
func (m *MemoryKeyStore) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.keys)
}

// maxKeysFile bounds the key file a read accepts.
const maxKeysFile = 1 << 20

// keysFile is the key file's content: the connection's id to its key.
type keysFile struct {
	Version int               `json:"version"`
	Keys    map[string]string `json:"keys"`
}

// FileKeyStore keeps the keys in one JSON file, owner-only: written through the engine's writer
// (0600 in a 0700 directory, replaced atomically, never through a symlink), read and written anew
// on every call, so that a second Cogpit's key survives this one's write. A file that other users
// may read or write (a copy restored without its mode, a chmod by hand) is made owner-only again
// before its keys are used, and the log says so, naming the file and its mode, never a key
// (docs/security/credentials.md, H-99). On Windows file modes do not express access; the user
// profile's ACLs apply.
type FileKeyStore struct {
	path string
	log  *slog.Logger
	mu   sync.Mutex
}

// NewFileKeyStore returns the key store kept in the file at path; logger (nil: slog's default)
// receives the repair of a file other users could read.
func NewFileKeyStore(path string, logger *slog.Logger) *FileKeyStore {
	if logger == nil {
		logger = slog.Default()
	}
	return &FileKeyStore{path: path, log: logger}
}

// Get implements KeyStore.
func (s *FileKeyStore) Get(account string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys, err := s.read()
	if err != nil {
		return "", err
	}
	k, ok := keys[account]
	if !ok || k == "" {
		return "", ErrNoKey
	}
	return k, nil
}

// Set implements KeyStore. A file that does not decode holds no key Cogpit can use: it is
// replaced.
func (s *FileKeyStore) Set(account, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys, err := s.readForWrite()
	if err != nil {
		return err
	}
	keys[account] = key
	return s.write(keys)
}

// Delete implements KeyStore.
func (s *FileKeyStore) Delete(account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys, err := s.readForWrite()
	if err != nil {
		return err
	}
	if _, ok := keys[account]; !ok {
		return nil
	}
	delete(keys, account)
	return s.write(keys)
}

// readForWrite is read, a file that does not decode taken as empty (the write replaces it).
func (s *FileKeyStore) readForWrite() (map[string]string, error) {
	keys, err := s.read()
	if errors.Is(err, errKeysFileInvalid) {
		s.log.Warn("the chat's API key file did not decode: the next key saved replaces it", "file", s.path)
		return map[string]string{}, nil
	}
	return keys, err
}

// read returns the file's keys; a missing file holds none. A symlink, a non-regular file and one
// larger than maxKeysFile are refused; a file others may access is made owner-only first.
func (s *FileKeyStore) read() (map[string]string, error) {
	fi, err := os.Lstat(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("the API key file: %w", err)
	}
	var f keysFile
	if err := engine.ReadPrivateJSON(s.path, maxKeysFile, &f); err != nil {
		var syntax *json.SyntaxError
		var typ *json.UnmarshalTypeError
		if errors.As(err, &syntax) || errors.As(err, &typ) {
			return nil, fmt.Errorf("the API key file %s: %w", s.path, errKeysFileInvalid)
		}
		return nil, fmt.Errorf("the API key file: %w", err)
	}
	keys := f.Keys
	if keys == nil {
		keys = map[string]string{}
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		// Repaired rather than refused: the file is Cogpit's own, as ensurePrivateDir tightens
		// Cogpit's directory, and a key that others may have read is no safer for being refused
		// (Cogpit cannot revoke it); the log tells the user to replace it.
		s.log.Warn("the chat's API key file was accessible by other users: Cogpit made it owner-only (0600); "+
			"replace its keys at their providers if other users of this computer may have read them",
			"file", s.path, "mode", fmt.Sprintf("%#o", fi.Mode().Perm()))
		if err := s.write(keys); err != nil {
			return nil, fmt.Errorf("the API key file is accessible by other users and could not be made owner-only: %w", err)
		}
	}
	return keys, nil
}

// write replaces the file with keys, owner-only.
func (s *FileKeyStore) write(keys map[string]string) error {
	data, err := json.MarshalIndent(keysFile{Version: fileVersion, Keys: keys}, "", "  ")
	if err != nil {
		return fmt.Errorf("the API key file: %w", err)
	}
	defer clear(data) // best effort: the encoded keys do not outlive the write in this buffer
	if err := engine.WritePrivateFile(s.path, data); err != nil {
		return fmt.Errorf("the API key file could not be written: %w", err)
	}
	return nil
}
