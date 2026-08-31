package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/rursache/always-green-cli/internal/paths"
	"github.com/rursache/always-green-cli/internal/schedule"
)

const (
	SourceDesktop = "desktop"
	SourcePaste   = "paste"
)

type Config struct {
	Timezone string `json:"timezone,omitempty"`
}

type UserInfo struct {
	Name        string `json:"name,omitempty"`
	RealName    string `json:"real_name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Email       string `json:"email,omitempty"`
}

type Workspace struct {
	Name            string           `json:"name"`
	TeamID          string           `json:"team_id"`
	UserID          string           `json:"user_id"`
	Xoxc            string           `json:"xoxc"`
	Xoxd            string           `json:"xoxd"`
	Paused          bool             `json:"is_paused"`
	Source          string           `json:"source,omitempty"`
	Domain          string           `json:"domain,omitempty"`
	TokenInvalid    bool             `json:"token_invalid,omitempty"`
	TokenInvalidAt  string           `json:"token_invalid_at,omitempty"`
	UserInfo        *UserInfo        `json:"user_info,omitempty"`
	Schedule        *schedule.Window `json:"schedule,omitempty"`
	KeepOnlineUntil string           `json:"keep_online_until,omitempty"`
	AddedAt         string           `json:"added_at,omitempty"`
	UpdatedAt       string           `json:"updated_at,omitempty"`
}

type workspaceFile struct {
	Workspaces []Workspace `json:"workspaces"`
}

type Store struct {
	mu  sync.Mutex
	key []byte
}

// withFileLock serialises a read-modify-write across processes. The daemon and
// every CLI/TUI invocation open their own Store, so the in-process mutex alone
// let two of them read the same state, each apply a change, and the last write
// win - silently dropping the other's edit.
func withFileLock(fn func() error) error {
	f, err := os.OpenFile(paths.StoreLock(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

func Open() (*Store, error) {
	if err := paths.EnsureDir(); err != nil {
		return nil, err
	}
	key, err := loadOrCreateKey()
	if err != nil {
		return nil, err
	}
	return &Store{key: key}, nil
}

func (s *Store) Config() (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cfg Config
	ok, err := s.read(paths.ConfigFile(), &cfg)
	if err != nil || !ok {
		return Config{}, err
	}
	return cfg, nil
}

func (s *Store) SaveConfig(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return withFileLock(func() error {
		return s.write(paths.ConfigFile(), cfg)
	})
}

func (s *Store) Workspaces() ([]Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var file workspaceFile
	ok, err := s.read(paths.WorkspacesFile(), &file)
	if err != nil || !ok {
		return nil, err
	}
	return file.Workspaces, nil
}

func (s *Store) SaveWorkspace(ws Workspace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return withFileLock(func() error { return s.saveWorkspace(ws) })
}

func (s *Store) saveWorkspace(ws Workspace) error {
	var file workspaceFile
	_, err := s.read(paths.WorkspacesFile(), &file)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	ws.UpdatedAt = now
	found := false
	for i, existing := range file.Workspaces {
		if existing.TeamID == ws.TeamID {
			if ws.AddedAt == "" {
				ws.AddedAt = existing.AddedAt
			}
			if ws.Schedule == nil {
				ws.Schedule = existing.Schedule
			}
			if ws.KeepOnlineUntil == "" {
				ws.KeepOnlineUntil = existing.KeepOnlineUntil
			}
			if ws.Source == "" {
				ws.Source = existing.Source
			}
			if ws.Domain == "" {
				ws.Domain = existing.Domain
			}
			ws.Paused = existing.Paused
			file.Workspaces[i] = ws
			found = true
			break
		}
	}
	if !found {
		if ws.AddedAt == "" {
			ws.AddedAt = now
		}
		file.Workspaces = append(file.Workspaces, ws)
	}
	return s.write(paths.WorkspacesFile(), file)
}

func (s *Store) UpdateWorkspace(teamID string, fn func(*Workspace)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return withFileLock(func() error { return s.updateWorkspace(teamID, fn) })
}

func (s *Store) updateWorkspace(teamID string, fn func(*Workspace)) error {
	var file workspaceFile
	ok, err := s.read(paths.WorkspacesFile(), &file)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("no workspaces saved")
	}
	for i := range file.Workspaces {
		if file.Workspaces[i].TeamID == teamID {
			fn(&file.Workspaces[i])
			file.Workspaces[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			return s.write(paths.WorkspacesFile(), file)
		}
	}
	return fmt.Errorf("workspace %s not found", teamID)
}

func (s *Store) RemoveWorkspace(teamID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return withFileLock(func() error { return s.removeWorkspace(teamID) })
}

func (s *Store) removeWorkspace(teamID string) error {
	var file workspaceFile
	ok, err := s.read(paths.WorkspacesFile(), &file)
	if err != nil || !ok {
		return err
	}
	out := file.Workspaces[:0]
	for _, ws := range file.Workspaces {
		if ws.TeamID != teamID {
			out = append(out, ws)
		}
	}
	file.Workspaces = out
	return s.write(paths.WorkspacesFile(), file)
}

func (s *Store) ClearWorkspaces() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// already-absent satisfies the postcondition
	if err := os.Remove(paths.WorkspacesFile()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (ws Workspace) KeepOnlineActive(now time.Time) bool {
	if ws.KeepOnlineUntil == "" {
		return false
	}
	until, err := time.Parse(time.RFC3339, ws.KeepOnlineUntil)
	if err != nil {
		return false
	}
	return until.After(now)
}

func (ws Workspace) Eligible(now time.Time, tz string) bool {
	if ws.TokenInvalid {
		return false
	}
	if ws.KeepOnlineActive(now) {
		return true
	}
	if ws.Paused {
		return false
	}
	return schedule.InWindow(ws.Schedule, now, tz)
}

func (s *Store) MarkTokenInvalid(teamID string) error {
	return s.UpdateWorkspace(teamID, func(w *Workspace) {
		w.TokenInvalid = true
		if w.TokenInvalidAt == "" {
			w.TokenInvalidAt = time.Now().UTC().Format(time.RFC3339)
		}
	})
}

func (s *Store) Workspace(teamID string) (Workspace, bool) {
	list, err := s.Workspaces()
	if err != nil {
		return Workspace{}, false
	}
	for _, ws := range list {
		if ws.TeamID == teamID {
			return ws, true
		}
	}
	return Workspace{}, false
}

func loadOrCreateKey() ([]byte, error) {
	path := paths.MasterKey()
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != 32 {
			return nil, errors.New("master.key is the wrong size")
		}
		// this key decrypts the Slack tokens; refuse to use one that other
		// local accounts can read rather than carrying on silently
		if st, statErr := os.Stat(path); statErr == nil && st.Mode().Perm()&0o077 != 0 {
			if chmodErr := os.Chmod(path, 0o600); chmodErr != nil {
				return nil, fmt.Errorf("%s is readable by other users and could not be tightened: %w", path, chmodErr)
			}
		}
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := writeFileSync(path, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func (s *Store) read(path string, dest any) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	plain, err := s.decrypt(raw)
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(plain, dest); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) write(path string, v any) error {
	plain, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	enc, err := s.encrypt(plain)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := writeFileSync(tmp, enc, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return syncDir(filepath.Dir(path))
}

// writeFileSync flushes to disk before returning, so a crash right after a
// save cannot leave the renamed file empty or holding stale cached content
func writeFileSync(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// syncDir persists the rename itself
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (s *Store) encrypt(plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

func (s *Store) decrypt(raw []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}
