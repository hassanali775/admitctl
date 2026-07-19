package onboarding

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/hassanli775/admitctl/internal/health"
	"github.com/hassanli775/admitctl/internal/registry"
	"github.com/hassanli775/admitctl/internal/store"
	"github.com/hassanli775/admitctl/internal/tenant"
)

func validCfg(id string) tenant.Config {
	return tenant.Config{
		ID:                id,
		DisplayName:       "Tenant " + id,
		Auth:              tenant.AuthAPIKey,
		DataSchemaVersion: "v1",
		RateLimit:         tenant.RateLimit{RequestsPerSecond: 10, Burst: 20},
	}
}

func TestRegisterStep_DoThenUndo(t *testing.T) {
	reg := registry.NewRegistry()
	step := &RegisterStep{Reg: reg, Config: validCfg("acme")}

	if err := step.Do(context.Background()); err != nil {
		t.Fatalf("Do failed: %v", err)
	}
	if _, err := reg.Get("acme"); err != nil {
		t.Fatalf("expected tenant present after Do, got: %v", err)
	}

	if err := step.Undo(context.Background()); err != nil {
		t.Fatalf("Undo failed: %v", err)
	}
	if _, err := reg.Get("acme"); !errors.Is(err, registry.ErrNotFound) {
		t.Fatal("expected tenant to be completely gone after Undo")
	}
}

func TestHealthCheckStep_UnhealthyFailsDo(t *testing.T) {
	reg := registry.NewRegistry()
	cfg := validCfg("acme")
	cfg.DataSchemaVersion = "v99" // not in the supported or deprecated sets below
	if err := reg.Register(cfg); err != nil {
		t.Fatalf("setup: register failed: %v", err)
	}

	runner := health.NewRunner(health.NewSchemaVersionChecker([]string{"v1"}, nil))
	step := &HealthCheckStep{Reg: reg, TenantID: "acme", Runner: runner}

	err := step.Do(context.Background())
	if err == nil {
		t.Fatal("expected an error for an unhealthy tenant")
	}
	var uerr *UnhealthyConfigError
	if !errors.As(err, &uerr) {
		t.Fatalf("expected *UnhealthyConfigError, got %T", err)
	}
	if uerr.Report.Overall != health.StatusUnhealthy {
		t.Fatalf("expected report overall unhealthy, got %s", uerr.Report.Overall)
	}
}

func TestHealthCheckStep_DegradedDoesNotFailDo(t *testing.T) {
	reg := registry.NewRegistry()
	cfg := validCfg("acme")
	cfg.RateLimit = tenant.RateLimit{RequestsPerSecond: 10, Burst: 10} // zero headroom -> degraded
	if err := reg.Register(cfg); err != nil {
		t.Fatalf("setup: register failed: %v", err)
	}

	runner := health.NewRunner(health.RateLimitHeadroomChecker{})
	step := &HealthCheckStep{Reg: reg, TenantID: "acme", Runner: runner}

	if err := step.Do(context.Background()); err != nil {
		t.Fatalf("expected Degraded to be non-fatal, got error: %v", err)
	}
	if step.Report.Overall != health.StatusDegraded {
		t.Fatalf("expected report overall degraded, got %s", step.Report.Overall)
	}
}

func TestHealthCheckStep_UndoIsNoOp(t *testing.T) {
	reg := registry.NewRegistry()
	if err := reg.Register(validCfg("acme")); err != nil {
		t.Fatalf("setup: register failed: %v", err)
	}
	step := &HealthCheckStep{Reg: reg, TenantID: "acme", Runner: health.NewRunner()}

	if err := step.Undo(context.Background()); err != nil {
		t.Fatalf("expected Undo to always succeed as a no-op, got: %v", err)
	}
	if _, err := reg.Get("acme"); err != nil {
		t.Fatalf("expected tenant untouched by Undo, got: %v", err)
	}
}

func TestPersistStep_DoWritesCurrentRegistryState(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tenants.json")
	reg := registry.NewRegistry()
	if err := reg.Register(validCfg("acme")); err != nil {
		t.Fatalf("setup: register failed: %v", err)
	}

	step := &PersistStep{StorePath: storePath, Reg: reg}
	if err := step.Do(context.Background()); err != nil {
		t.Fatalf("Do failed: %v", err)
	}

	records, err := store.Load(storePath)
	if err != nil {
		t.Fatalf("expected saved file to load, got: %v", err)
	}
	if len(records) != 1 || records[0].Config.ID != "acme" {
		t.Fatalf("expected persisted acme record, got: %+v", records)
	}
}

func TestPersistStep_UndoRestoresPreviousSnapshot(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tenants.json")

	// Pre-existing state before this onboarding attempt started.
	pre := registry.NewRegistry()
	if err := pre.Register(validCfg("existing")); err != nil {
		t.Fatalf("setup: register failed: %v", err)
	}
	if err := store.Save(storePath, pre.List()); err != nil {
		t.Fatalf("setup: save failed: %v", err)
	}

	// Now simulate an onboarding attempt: registry gains a new
	// tenant, PersistStep.Do writes it out...
	reg := registry.NewRegistry()
	reg.Restore(pre.List())
	if err := reg.Register(validCfg("new-tenant")); err != nil {
		t.Fatalf("register new tenant failed: %v", err)
	}
	step := &PersistStep{StorePath: storePath, Reg: reg}
	if err := step.Do(context.Background()); err != nil {
		t.Fatalf("Do failed: %v", err)
	}

	// ...then a later step fails, and PersistStep.Undo must restore
	// the file to exactly what it held before Do ran — "existing"
	// only, no "new-tenant" — regardless of what the in-memory
	// registry looks like by the time Undo runs.
	if err := step.Undo(context.Background()); err != nil {
		t.Fatalf("Undo failed: %v", err)
	}

	records, err := store.Load(storePath)
	if err != nil {
		t.Fatalf("expected file to still load after Undo, got: %v", err)
	}
	if len(records) != 1 || records[0].Config.ID != "existing" {
		t.Fatalf("expected only the pre-existing tenant after Undo, got: %+v", records)
	}
}

func TestPersistStep_UndoWithNoPriorFileRestoresEmpty(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tenants.json") // never written before

	reg := registry.NewRegistry()
	if err := reg.Register(validCfg("acme")); err != nil {
		t.Fatalf("setup: register failed: %v", err)
	}
	step := &PersistStep{StorePath: storePath, Reg: reg}
	if err := step.Do(context.Background()); err != nil {
		t.Fatalf("Do failed: %v", err)
	}

	if err := step.Undo(context.Background()); err != nil {
		t.Fatalf("Undo failed: %v", err)
	}

	records, err := store.Load(storePath)
	if err != nil {
		t.Fatalf("expected file to still load after Undo, got: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected no records after undoing a first-ever persist, got: %+v", records)
	}
}