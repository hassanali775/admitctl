// Package store persists registry.Record snapshots to a local JSON
// file so a Registry's contents survive across CLI invocations. It
// deliberately knows nothing about tenant validation or business
// rules — it is a dumb, swappable persistence adapter, matching the
// same "swap the backend later" philosophy the registry package
// documents for its own storage.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hassanli775/admitctl/internal/registry"
)

// ErrNotExist indicates the store file has never been written yet.
// Callers should treat this as "start with an empty registry," not
// as a failure.
var ErrNotExist = errors.New("store: file does not exist")

// Load reads and decodes the record snapshot at path. It returns
// ErrNotExist if path has not been created yet.
func Load(path string) ([]registry.Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotExist
		}
		return nil, fmt.Errorf("store: read %s: %w", path, err)
	}

	var records []registry.Record
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("store: decode %s: %w", path, err)
	}
	return records, nil
}

// Save writes records to path as indented JSON. It writes to a
// temporary file in the same directory and renames it into place, so
// a process crash or power loss mid-write cannot leave a truncated or
// corrupt store file behind.
func Save(path string, records []registry.Record) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("store: create directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("store: encode records: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".tenants-*.json.tmp")
	if err != nil {
		return fmt.Errorf("store: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("store: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("store: close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("store: rename into place: %w", err)
	}
	return nil
}