package registry

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/hassanli775/admitctl/internal/tenant"
)

func sampleConfig(id string) tenant.Config {
	return tenant.Config{
		ID:                id,
		DisplayName:       "Sample Tenant " + id,
		Auth:              tenant.AuthAPIKey,
		DataSchemaVersion: "v1",
		RateLimit:         tenant.RateLimit{RequestsPerSecond: 50, Burst: 100},
	}
}

func TestRegister_Success(t *testing.T) {
	r := NewRegistry()
	cfg := sampleConfig("acme")

	if err := r.Register(cfg); err != nil {
		t.Fatalf("expected successful registration, got: %v", err)
	}

	rec, err := r.Get("acme")
	if err != nil {
		t.Fatalf("expected to find registered tenant, got: %v", err)
	}
	if rec.Status != StatusActive {
		t.Fatalf("expected newly registered tenant to be active, got: %s", rec.Status)
	}
	if rec.Config.ID != "acme" {
		t.Fatalf("expected stored config ID %q, got %q", "acme", rec.Config.ID)
	}
}

func TestRegister_InvalidConfigRejected(t *testing.T) {
	r := NewRegistry()
	invalid := sampleConfig("acme")
	invalid.Auth = tenant.AuthMethod("not-a-real-method")

	err := r.Register(invalid)
	if err == nil {
		t.Fatal("expected registration of invalid config to fail")
	}
	var verrs tenant.ValidationErrors
	if !errors.As(err, &verrs) {
		t.Fatalf("expected underlying error to be tenant.ValidationErrors, got %T", err)
	}

	if _, getErr := r.Get("acme"); !errors.Is(getErr, ErrNotFound) {
		t.Fatal("invalid config must not be stored in the registry")
	}
}

func TestRegister_DuplicateIDRejected(t *testing.T) {
	r := NewRegistry()
	cfg := sampleConfig("acme")

	if err := r.Register(cfg); err != nil {
		t.Fatalf("first registration should succeed, got: %v", err)
	}
	err := r.Register(cfg)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists on duplicate registration, got: %v", err)
	}
}

func TestGet_NotFound(t *testing.T) {
	r := NewRegistry()
	_, err := r.Get("does-not-exist")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestList_EmptyRegistry(t *testing.T) {
	r := NewRegistry()
	if got := r.List(); len(got) != 0 {
		t.Fatalf("expected empty registry to list 0 tenants, got %d", len(got))
	}
}

func TestList_SortedByID(t *testing.T) {
	r := NewRegistry()
	ids := []string{"zeta", "alpha", "mike"}
	for _, id := range ids {
		if err := r.Register(sampleConfig(id)); err != nil {
			t.Fatalf("failed to register %q: %v", id, err)
		}
	}

	got := r.List()
	if len(got) != len(ids) {
		t.Fatalf("expected %d records, got %d", len(ids), len(got))
	}
	want := []string{"alpha", "mike", "zeta"}
	for i, rec := range got {
		if rec.Config.ID != want[i] {
			t.Fatalf("expected sorted order %v, got position %d = %q", want, i, rec.Config.ID)
		}
	}
}

func TestDeactivate_Success(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(sampleConfig("acme")); err != nil {
		t.Fatalf("registration failed: %v", err)
	}

	if err := r.Deactivate("acme"); err != nil {
		t.Fatalf("expected deactivate to succeed, got: %v", err)
	}

	rec, err := r.Get("acme")
	if err != nil {
		t.Fatalf("expected tenant to still exist after deactivation, got: %v", err)
	}
	if rec.Status != StatusInactive {
		t.Fatalf("expected status inactive after deactivation, got: %s", rec.Status)
	}
}

func TestDeactivate_IdempotentOnAlreadyInactive(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(sampleConfig("acme")); err != nil {
		t.Fatalf("registration failed: %v", err)
	}
	if err := r.Deactivate("acme"); err != nil {
		t.Fatalf("first deactivate failed: %v", err)
	}
	if err := r.Deactivate("acme"); err != nil {
		t.Fatalf("expected second deactivate to be a no-op, got: %v", err)
	}
}

func TestDeactivate_NotFound(t *testing.T) {
	r := NewRegistry()
	err := r.Deactivate("does-not-exist")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestRestore_HydratesFromSnapshot(t *testing.T) {
	src := NewRegistry()
	if err := src.Register(sampleConfig("acme")); err != nil {
		t.Fatalf("setup: register failed: %v", err)
	}
	if err := src.Deactivate("acme"); err != nil {
		t.Fatalf("setup: deactivate failed: %v", err)
	}
	if err := src.Register(sampleConfig("globex")); err != nil {
		t.Fatalf("setup: register failed: %v", err)
	}
	snapshot := src.List()

	dst := NewRegistry()
	dst.Restore(snapshot)

	got := dst.List()
	if len(got) != 2 {
		t.Fatalf("expected 2 restored records, got %d", len(got))
	}

	acme, err := dst.Get("acme")
	if err != nil {
		t.Fatalf("expected restored tenant %q to be present: %v", "acme", err)
	}
	if acme.Status != StatusInactive {
		t.Fatalf("expected restored status to be preserved (inactive), got %s", acme.Status)
	}

	globex, err := dst.Get("globex")
	if err != nil {
		t.Fatalf("expected restored tenant %q to be present: %v", "globex", err)
	}
	if globex.Status != StatusActive {
		t.Fatalf("expected restored status to be preserved (active), got %s", globex.Status)
	}
}

func TestRestore_ReplacesExistingContents(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(sampleConfig("stale")); err != nil {
		t.Fatalf("setup: register failed: %v", err)
	}

	r.Restore([]Record{}) // empty snapshot

	if got := r.List(); len(got) != 0 {
		t.Fatalf("expected Restore with empty snapshot to clear existing records, got %d", len(got))
	}
}

// TestRegistry_ConcurrentAccess registers, reads, lists, and
// deactivates tenants from many goroutines at once. Run with -race
// to confirm the registry's locking is correct, not just its logic.
func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	const n = 50

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("tenant-%02d", i)
			if err := r.Register(sampleConfig(id)); err != nil {
				t.Errorf("register %q failed: %v", id, err)
				return
			}
			if _, err := r.Get(id); err != nil {
				t.Errorf("get %q failed: %v", id, err)
			}
			_ = r.List()
			if err := r.Deactivate(id); err != nil {
				t.Errorf("deactivate %q failed: %v", id, err)
			}
		}(i)
	}
	wg.Wait()

	got := r.List()
	if len(got) != n {
		t.Fatalf("expected %d tenants after concurrent registration, got %d", n, len(got))
	}
	for _, rec := range got {
		if rec.Status != StatusInactive {
			t.Fatalf("expected tenant %q to be inactive, got %s", rec.Config.ID, rec.Status)
		}
	}
}