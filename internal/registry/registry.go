// Package registry provides a concurrency-safe, in-memory store of
// tenant records. It is the source of truth the onboarding CLI,
// health-check subsystem, and (eventually) rollback logic all read
// from and write to.
//
// The in-memory implementation is intentional for this stage of the
// project: it lets the registration/lookup/deactivation contract get
// nailed down and tested before a persistent backend (e.g. a
// key-value store or SQL) is swapped in behind the same interface.
package registry

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/hassanli775/admitctl/internal/tenant"
)

// ErrNotFound is returned when an operation references a tenant ID
// that isn't present in the registry.
var ErrNotFound = errors.New("tenant not found")

// ErrAlreadyExists is returned by Register when a tenant with the
// same ID has already been registered.
var ErrAlreadyExists = errors.New("tenant already exists")

// Status is the lifecycle state of a registered tenant.
type Status string

const (
	StatusActive   Status = "active"
	StatusInactive Status = "inactive"
)

// Record is what the registry actually stores: a tenant's onboarding
// config plus registry-owned bookkeeping (status, timestamps) that
// has no business living in tenant.Config itself.
type Record struct {
	Config    tenant.Config
	Status    Status
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Registry is a concurrency-safe, in-memory tenant store. The zero
// value is not usable; construct one with NewRegistry.
type Registry struct {
	mu      sync.RWMutex
	records map[string]*Record
}

// NewRegistry returns an empty, ready-to-use Registry.
func NewRegistry() *Registry {
	return &Registry{
		records: make(map[string]*Record),
	}
}

// Register validates cfg and adds it to the registry as an active
// tenant. It returns cfg's validation error unchanged if cfg is
// invalid, or a wrapped ErrAlreadyExists if cfg.ID is already
// registered. Register never mutates an existing record — callers
// who need to change a tenant's config should introduce an explicit
// Update method rather than re-Register.
func (r *Registry) Register(cfg tenant.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.records[cfg.ID]; exists {
		return fmt.Errorf("registry: register %q: %w", cfg.ID, ErrAlreadyExists)
	}

	now := time.Now()
	r.records[cfg.ID] = &Record{
		Config:    cfg,
		Status:    StatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	return nil
}

// Get returns a copy of the record for id, or a wrapped ErrNotFound
// if no such tenant is registered.
func (r *Registry) Get(id string) (Record, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	rec, ok := r.records[id]
	if !ok {
		return Record{}, fmt.Errorf("registry: get %q: %w", id, ErrNotFound)
	}
	return *rec, nil
}

// List returns a copy of every registered record (active and
// inactive), sorted by tenant ID for deterministic output. Callers
// that only want active tenants should filter the result — List
// intentionally stays unopinionated about status.
func (r *Registry) List() []Record {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Record, 0, len(r.records))
	for _, rec := range r.records {
		out = append(out, *rec)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Config.ID < out[j].Config.ID
	})
	return out
}

// Deactivate marks an existing tenant as inactive. It is idempotent:
// deactivating an already-inactive tenant succeeds without error.
// Deactivate returns a wrapped ErrNotFound if id isn't registered.
func (r *Registry) Deactivate(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	rec, ok := r.records[id]
	if !ok {
		return fmt.Errorf("registry: deactivate %q: %w", id, ErrNotFound)
	}
	if rec.Status == StatusInactive {
		return nil
	}
	rec.Status = StatusInactive
	rec.UpdatedAt = time.Now()
	return nil
}

// Restore replaces the registry's entire contents with records,
// bypassing Register's validation and duplicate checks. It exists
// solely to hydrate a fresh Registry from a persisted snapshot (see
// internal/store) at process startup. Callers representing a new
// tenant being onboarded should use Register, never Restore.
func (r *Registry) Restore(records []Record) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.records = make(map[string]*Record, len(records))
	for i := range records {
		rec := records[i]
		r.records[rec.Config.ID] = &rec
	}
}