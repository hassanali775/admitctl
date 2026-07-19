package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hassanli775/admitctl/internal/registry"
	"github.com/hassanli775/admitctl/internal/tenant"
)

func writeRaw(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func sampleRecords() []registry.Record {
	now := time.Now().Truncate(0) // strip monotonic reading for stable JSON round-trips
	return []registry.Record{
		{
			Config: tenant.Config{
				ID:                "acme",
				DisplayName:       "Acme Corp",
				Auth:              tenant.AuthAPIKey,
				DataSchemaVersion: "v1",
				RateLimit:         tenant.RateLimit{RequestsPerSecond: 50, Burst: 100},
				FeatureFlags:      map[string]bool{"beta_dashboard": true},
			},
			Status:    registry.StatusActive,
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			Config: tenant.Config{
				ID:                "globex",
				DisplayName:       "Globex Corporation",
				Auth:              tenant.AuthMTLS,
				DataSchemaVersion: "v2",
				RateLimit:         tenant.RateLimit{RequestsPerSecond: 10, Burst: 10},
			},
			Status:    registry.StatusInactive,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenants.json")
	want := sampleRecords()

	if err := Save(path, want); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("expected %d records, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i].Config.ID != want[i].Config.ID {
			t.Errorf("record %d: expected ID %q, got %q", i, want[i].Config.ID, got[i].Config.ID)
		}
		if got[i].Status != want[i].Status {
			t.Errorf("record %d: expected status %q, got %q", i, want[i].Status, got[i].Status)
		}
		if !got[i].CreatedAt.Equal(want[i].CreatedAt) {
			t.Errorf("record %d: expected CreatedAt %v, got %v", i, want[i].CreatedAt, got[i].CreatedAt)
		}
	}
}

func TestLoad_MissingFileReturnsErrNotExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")

	_, err := Load(path)
	if !errors.Is(err, ErrNotExist) {
		t.Fatalf("expected ErrNotExist, got: %v", err)
	}
}

func TestLoad_CorruptFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tenants.json")

	if err := writeRaw(path, "{not valid json"); err != nil {
		t.Fatalf("test setup failed: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected an error decoding a corrupt store file, got nil")
	}
	if errors.Is(err, ErrNotExist) {
		t.Fatal("corrupt file should not be reported as ErrNotExist")
	}
}

func TestSave_CreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "tenants.json")

	if err := Save(path, sampleRecords()); err != nil {
		t.Fatalf("Save should create missing parent directories, got: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("expected to load what was just saved, got: %v", err)
	}
}

func TestSave_EmptySliceProducesLoadableEmptyResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenants.json")

	if err := Save(path, []registry.Record{}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 records, got %d", len(got))
	}
}