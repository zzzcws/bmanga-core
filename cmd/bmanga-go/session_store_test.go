package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionStoreMaxEntriesUsesBoundedConfiguration(t *testing.T) {
	cases := []struct {
		value string
		want  int
	}{
		{"", defaultSessionStoreMaxEntries},
		{"16", minimumSessionStoreMaxEntries},
		{"512", maximumSessionStoreMaxEntries},
		{"15", defaultSessionStoreMaxEntries},
		{"513", defaultSessionStoreMaxEntries},
		{"invalid", defaultSessionStoreMaxEntries},
	}
	for _, testCase := range cases {
		t.Run(testCase.value, func(t *testing.T) {
			t.Setenv("BMANGA_SESSION_MAX_ENTRIES", testCase.value)
			if got := sessionStoreMaxEntries(); got != testCase.want {
				t.Fatalf("max entries = %d, want %d", got, testCase.want)
			}
		})
	}
}

func TestPersistentSessionStoreEvictsEarliestExpiryAndSurvivesRestart(t *testing.T) {
	t.Setenv("BMANGA_SESSION_MAX_ENTRIES", "16")
	storePath := filepath.Join(t.TempDir(), sessionStoreFileName)
	store, err := newPersistentSessionStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Unix(time.Now().Add(2*time.Hour).Unix(), 0)
	for index := 0; index < minimumSessionStoreMaxEntries; index++ {
		if err := store.add(sessionTestNonce(index), base.Add(time.Duration(index)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	newNonce := "new-session"
	newExpiry := base.Add(24 * time.Hour)
	if err := store.add(newNonce, newExpiry); err != nil {
		t.Fatal(err)
	}
	if got := len(store.expires); got != minimumSessionStoreMaxEntries {
		t.Fatalf("session count = %d, want %d", got, minimumSessionStoreMaxEntries)
	}
	if store.valid(sessionTestNonce(0), base) {
		t.Fatal("earliest-expiring session should be evicted")
	}
	if !store.valid(sessionTestNonce(1), base.Add(time.Minute)) {
		t.Fatal("later existing session should remain valid")
	}
	if !store.valid(newNonce, newExpiry) {
		t.Fatal("new session should remain valid after capacity eviction")
	}

	restarted, err := newPersistentSessionStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(restarted.expires); got != minimumSessionStoreMaxEntries {
		t.Fatalf("restarted session count = %d, want %d", got, minimumSessionStoreMaxEntries)
	}
	if restarted.valid(sessionTestNonce(0), base) {
		t.Fatal("evicted session reappeared after restart")
	}
	if !restarted.valid(newNonce, newExpiry) {
		t.Fatal("new session did not survive restart")
	}
}

func TestPersistentSessionStorePrunesExpiredAndOverCapacityEntriesOnLoad(t *testing.T) {
	t.Setenv("BMANGA_SESSION_MAX_ENTRIES", "16")
	storePath := filepath.Join(t.TempDir(), sessionStoreFileName)
	now := time.Now()
	persisted := persistedSessionStore{
		Version:  sessionStoreFormatVersion,
		Sessions: map[string]int64{},
	}
	persisted.Sessions[sessionStoreKey("expired")] = now.Add(-time.Hour).Unix()
	for index := 0; index < minimumSessionStoreMaxEntries+1; index++ {
		persisted.Sessions[sessionStoreKey(sessionTestNonce(index))] = now.Add(time.Duration(index+1) * time.Hour).Unix()
	}
	body, err := json.Marshal(persisted)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storePath, append(body, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := newPersistentSessionStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(store.expires); got != minimumSessionStoreMaxEntries {
		t.Fatalf("loaded session count = %d, want %d", got, minimumSessionStoreMaxEntries)
	}
	if _, ok := store.expires[sessionStoreKey("expired")]; ok {
		t.Fatal("expired session was not pruned on load")
	}
	if _, ok := store.expires[sessionStoreKey(sessionTestNonce(0))]; ok {
		t.Fatal("earliest valid session was not evicted on load")
	}

	rewrittenBody, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatal(err)
	}
	var rewritten persistedSessionStore
	if err := json.Unmarshal(rewrittenBody, &rewritten); err != nil {
		t.Fatal(err)
	}
	if got := len(rewritten.Sessions); got != minimumSessionStoreMaxEntries {
		t.Fatalf("rewritten session count = %d, want %d", got, minimumSessionStoreMaxEntries)
	}
}

func sessionTestNonce(index int) string {
	return "session-" + time.Unix(int64(index), 0).UTC().Format("150405.000000000")
}
