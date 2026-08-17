package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const sessionStoreFileName = "auth-sessions.json"
const sessionStoreFormatVersion = 1
const defaultSessionStoreMaxEntries = 64
const minimumSessionStoreMaxEntries = 16
const maximumSessionStoreMaxEntries = 512

type sessionStore struct {
	mu         sync.Mutex
	path       string
	maxEntries int
	expires    map[string]time.Time
}

type persistedSessionStore struct {
	Version  int              `json:"version"`
	Sessions map[string]int64 `json:"sessions"`
}

func sessionStorePath(dbPath string) string {
	if configured := strings.TrimSpace(os.Getenv("BMANGA_SESSION_STORE_FILE")); configured != "" {
		return configured
	}
	dir := filepath.Dir(strings.TrimSpace(dbPath))
	if dir == "" || dir == "." {
		dir = "data"
	}
	return filepath.Join(dir, sessionStoreFileName)
}

func newSessionStore() *sessionStore {
	return &sessionStore{
		maxEntries: sessionStoreMaxEntries(),
		expires:    map[string]time.Time{},
	}
}

func sessionStoreMaxEntries() int {
	return envIntInRangeOrDefault(
		"BMANGA_SESSION_MAX_ENTRIES",
		defaultSessionStoreMaxEntries,
		minimumSessionStoreMaxEntries,
		maximumSessionStoreMaxEntries,
	)
}

func newPersistentSessionStore(path string) (*sessionStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("persistent session store path is empty")
	}
	path = filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create session store directory: %w", err)
	}
	store := &sessionStore{
		path:       path,
		maxEntries: sessionStoreMaxEntries(),
		expires:    map[string]time.Time{},
	}
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read session store: %w", err)
	}
	var persisted persistedSessionStore
	if err := json.Unmarshal(body, &persisted); err != nil {
		return nil, fmt.Errorf("decode session store: %w", err)
	}
	if persisted.Version != sessionStoreFormatVersion {
		return nil, fmt.Errorf("unsupported session store version %d", persisted.Version)
	}
	now := time.Now()
	changed := false
	for key, expiresUnix := range persisted.Sessions {
		if !validSessionStoreKey(key) {
			return nil, errors.New("session store contains an invalid session key")
		}
		expires := time.Unix(expiresUnix, 0)
		if now.Before(expires) {
			store.expires[key] = expires
		} else {
			changed = true
		}
	}
	if store.enforceLimitLocked() {
		changed = true
	}
	if changed {
		if err := store.persistLocked(); err != nil {
			return nil, fmt.Errorf("prune persistent session store: %w", err)
		}
	}
	return store, nil
}

func sessionStoreKey(nonce string) string {
	sum := sha256.Sum256([]byte(nonce))
	return hex.EncodeToString(sum[:])
}

func validSessionStoreKey(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (s *sessionStore) add(nonce string, expires time.Time) error {
	if s == nil {
		return errors.New("session store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := cloneSessionExpirations(s.expires)
	s.pruneExpiredLocked(time.Now())
	key := sessionStoreKey(nonce)
	if _, existed := s.expires[key]; !existed && len(s.expires) >= s.entryLimit() {
		s.evictEarliestLocked(s.entryLimit() - 1)
	}
	s.expires[key] = expires
	if err := s.persistLocked(); err != nil {
		s.expires = previous
		return err
	}
	return nil
}

func (s *sessionStore) valid(nonce string, expires time.Time) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := s.pruneExpiredLocked(time.Now())
	if s.enforceLimitLocked() {
		changed = true
	}
	stored, ok := s.expires[sessionStoreKey(nonce)]
	if changed {
		_ = s.persistLocked()
	}
	return ok && stored.Equal(expires)
}

func (s *sessionStore) revokeRequest(r *http.Request) error {
	if s == nil || r == nil {
		return nil
	}
	cookie, err := r.Cookie("bmanga_session")
	if err != nil {
		return nil
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 3 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := sessionStoreKey(parts[1])
	previous, existed := s.expires[key]
	if !existed {
		return nil
	}
	delete(s.expires, key)
	if err := s.persistLocked(); err != nil {
		s.expires[key] = previous
		return err
	}
	return nil
}

func (s *sessionStore) pruneExpiredLocked(now time.Time) bool {
	changed := false
	for key, expires := range s.expires {
		if !now.Before(expires) {
			delete(s.expires, key)
			changed = true
		}
	}
	return changed
}

func (s *sessionStore) entryLimit() int {
	if s.maxEntries < minimumSessionStoreMaxEntries || s.maxEntries > maximumSessionStoreMaxEntries {
		return defaultSessionStoreMaxEntries
	}
	return s.maxEntries
}

func (s *sessionStore) enforceLimitLocked() bool {
	return s.evictEarliestLocked(s.entryLimit())
}

func (s *sessionStore) evictEarliestLocked(target int) bool {
	changed := false
	for len(s.expires) > target {
		earliestKey := ""
		earliestExpiry := time.Time{}
		for key, expires := range s.expires {
			if earliestKey == "" || expires.Before(earliestExpiry) || (expires.Equal(earliestExpiry) && key < earliestKey) {
				earliestKey = key
				earliestExpiry = expires
			}
		}
		if earliestKey == "" {
			break
		}
		delete(s.expires, earliestKey)
		changed = true
	}
	return changed
}

func cloneSessionExpirations(source map[string]time.Time) map[string]time.Time {
	cloned := make(map[string]time.Time, len(source))
	for key, expires := range source {
		cloned[key] = expires
	}
	return cloned
}

func (s *sessionStore) persistLocked() error {
	s.pruneExpiredLocked(time.Now())
	s.enforceLimitLocked()
	if s.path == "" {
		return nil
	}
	persisted := persistedSessionStore{
		Version:  sessionStoreFormatVersion,
		Sessions: make(map[string]int64, len(s.expires)),
	}
	for key, expires := range s.expires {
		persisted.Sessions[key] = expires.Unix()
	}
	body, err := json.Marshal(persisted)
	if err != nil {
		return fmt.Errorf("encode session store: %w", err)
	}
	body = append(body, '\n')
	if err := writeSessionStoreAtomically(s.path, body); err != nil {
		return fmt.Errorf("write session store: %w", err)
	}
	return nil
}

func writeSessionStoreAtomically(path string, body []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".auth-sessions-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := replaceSessionStoreFile(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	if dirHandle, err := os.Open(dir); err == nil {
		_ = dirHandle.Sync()
		_ = dirHandle.Close()
	}
	return nil
}

func replaceSessionStoreFile(tmpPath string, path string) error {
	if err := os.Rename(tmpPath, path); err == nil {
		return nil
	}
	backupPath := path + ".previous"
	_ = os.Remove(backupPath)
	if err := os.Rename(path, backupPath); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Rename(backupPath, path)
		return err
	}
	_ = os.Remove(backupPath)
	return nil
}
