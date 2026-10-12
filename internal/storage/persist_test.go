package storage

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLitePersistence_PasswordHash(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.db")

	p, err := NewSQLitePersistence(dbPath)
	if err != nil {
		t.Fatalf("NewSQLitePersistence failed: %v", err)
	}

	st := &PersistedState{
		Version: 1,
		DirLinks: []PersistedLink{
			{
				Token:        "tok1",
				Path:         "/myfolder",
				ExpiresAt:    9999999999,
				CreatedAt:    1111111111,
				PasswordHash: "secret_hash_123",
			},
		},
		ShortLinks: []PersistedLink{
			{
				Token:        "tok2",
				Path:         "/myfolder/file.zip",
				ExpiresAt:    9999999999,
				CreatedAt:    1111111111,
				PasswordHash: "pwd_file_456",
				MaxUses:      5,
				Uses:         1,
			},
		},
		DirCache: []PersistedDirCache{
			{
				Dir:      "/myfolder",
				Body:     json.RawMessage(`{"errno":0,"list":[{"fs_id":888,"path":"/myfolder/a.txt"}]}`),
				CachedAt: time.Now().Unix(),
			},
		},
	}

	if err := p.Save(st); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	_ = p.Close()

	// Load back
	p2, err := NewSQLitePersistence(dbPath)
	if err != nil {
		t.Fatalf("NewSQLitePersistence 2 failed: %v", err)
	}
	loaded, err := p2.Load()
	_ = p2.Close()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded.DirLinks) != 1 || loaded.DirLinks[0].PasswordHash != "secret_hash_123" {
		t.Fatalf("unexpected DirLinks: %+v", loaded.DirLinks)
	}
	if len(loaded.ShortLinks) != 1 || loaded.ShortLinks[0].PasswordHash != "pwd_file_456" || loaded.ShortLinks[0].MaxUses != 5 {
		t.Fatalf("unexpected ShortLinks: %+v", loaded.ShortLinks)
	}
	if len(loaded.DirCache) != 1 || loaded.DirCache[0].Dir != "/myfolder" {
		t.Fatalf("unexpected DirCache: %+v", loaded.DirCache)
	}
}

func TestSQLitePersistence_DirCache(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.db")

	p, err := NewSQLitePersistence(dbPath)
	if err != nil {
		t.Fatalf("NewSQLitePersistence failed: %v", err)
	}
	defer p.Close()

	testJSON := []byte(`{"errno":0,"guid_info":{},"list":[{"fs_id":123,"path":"/test/file.txt"}]}`)
	st := &PersistedState{
		Version: 1,
		DirCache: []PersistedDirCache{
			{
				Dir:      "/test",
				Body:     testJSON,
				CachedAt: time.Now().Unix(),
			},
		},
	}

	if err := p.Save(st); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := p.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded.DirCache) != 1 {
		t.Fatalf("expected 1 dir cache entry, got %d", len(loaded.DirCache))
	}
	if loaded.DirCache[0].Dir != "/test" {
		t.Errorf("expected dir '/test', got %q", loaded.DirCache[0].Dir)
	}
}
