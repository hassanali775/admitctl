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

func defaultTestRunner() *health.Runner {
	return health.NewRunner(
		health.ConfigValidationChecker{},
		health.NewSchemaVersionChecker([]string{"v1"}, nil),
		health.RateLimitHeadroomChecker{},
	)
}

func TestOnboard_HealthyTenant_FullySucceeds(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tenants.json")
	reg := registry.NewRegistry()

	pipeline, hc := NewOnboardPipeline(reg, storePath, validCfg("acme"), defaultTestRunner())
	if err := pipeline.Run(context.Background()); err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if hc.Report.Overall != health.StatusHealthy {
		t.Fatalf("expected healthy report, got %s", hc.Report.Overall)
	}

	if _, err := reg.Get("acme"); err != nil {
		t.Fatalf("expected tenant registered, got: %v", err)
	}
	records, err := store.Load(storePath)
	if err != nil {
		t.Fatalf("expected persisted file, got: %v", err)
	}
	if len(records) != 1 || records[0].Config.ID != "acme" {
		t.Fatalf("expected acme persisted, got: %+v", records)
	}
}

func TestOnboard_UnhealthyTenant_RollsBackCompletely(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tenants.json")
	reg := registry.NewRegistry()

	badCfg := validCfg("bad-tenant")
	badCfg.DataSchemaVersion = "v99" // unrecognized -> unhealthy

	pipeline, hc := NewOnboardPipeline(reg, storePath, badCfg, defaultTestRunner())
	err := pipeline.Run(context.Background())
	if err == nil {
		t.Fatal("expected onboarding to fail for an unhealthy tenant")
	}

	var perr *PipelineError
	if !errors.As(err, &perr) {
		t.Fatalf("expected *PipelineError, got %T", err)
	}
	if perr.FailedStep != "initial-health-check" {
		t.Fatalf("expected failure at initial-health-check, got %q", perr.FailedStep)
	}
	if perr.RollbackErr != nil {
		t.Fatalf("expected clean rollback, got: %v", perr.RollbackErr)
	}
	if hc.Report.Overall != health.StatusUnhealthy {
		t.Fatalf("expected unhealthy report, got %s", hc.Report.Overall)
	}

	// The tenant must be completely gone from the registry — not
	// deactivated, gone — and never persisted at all.
	if _, err := reg.Get("bad-tenant"); !errors.Is(err, registry.ErrNotFound) {
		t.Fatal("expected rolled-back tenant to be entirely absent from the registry")
	}
	if _, err := store.Load(storePath); !errors.Is(err, store.ErrNotExist) {
		t.Fatal("expected no store file to have been created, since Persist never ran")
	}
}

func TestOnboard_UnhealthyTenant_DoesNotDisturbExistingTenants(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tenants.json")
	reg := registry.NewRegistry()

	// Onboard a good tenant first, successfully.
	goodPipeline, _ := NewOnboardPipeline(reg, storePath, validCfg("good-tenant"), defaultTestRunner())
	if err := goodPipeline.Run(context.Background()); err != nil {
		t.Fatalf("expected first onboarding to succeed, got: %v", err)
	}

	// Now attempt a bad one.
	badCfg := validCfg("bad-tenant")
	badCfg.DataSchemaVersion = "v99"
	badPipeline, _ := NewOnboardPipeline(reg, storePath, badCfg, defaultTestRunner())
	if err := badPipeline.Run(context.Background()); err == nil {
		t.Fatal("expected second onboarding to fail")
	}

	// good-tenant must be untouched in both registry and file.
	if _, err := reg.Get("good-tenant"); err != nil {
		t.Fatalf("expected good-tenant to remain registered, got: %v", err)
	}
	if _, err := reg.Get("bad-tenant"); !errors.Is(err, registry.ErrNotFound) {
		t.Fatal("expected bad-tenant to be entirely absent")
	}

	records, err := store.Load(storePath)
	if err != nil {
		t.Fatalf("expected store file to still exist from the first onboarding, got: %v", err)
	}
	if len(records) != 1 || records[0].Config.ID != "good-tenant" {
		t.Fatalf("expected only good-tenant persisted, got: %+v", records)
	}
}

func TestOnboard_InvalidConfig_FailsAtRegisterStepWithNoRollbackNeeded(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tenants.json")
	reg := registry.NewRegistry()

	invalid := validCfg("acme")
	invalid.Auth = tenant.AuthMethod("not-a-real-method")

	pipeline, _ := NewOnboardPipeline(reg, storePath, invalid, defaultTestRunner())
	err := pipeline.Run(context.Background())

	var perr *PipelineError
	if !errors.As(err, &perr) {
		t.Fatalf("expected *PipelineError, got %T", err)
	}
	if perr.FailedStep != "register" {
		t.Fatalf("expected failure at register step, got %q", perr.FailedStep)
	}
	var verrs tenant.ValidationErrors
	if !errors.As(perr.Cause, &verrs) {
		t.Fatalf("expected Cause to be tenant.ValidationErrors, got %T", perr.Cause)
	}
	if perr.RollbackErr != nil {
		t.Fatalf("expected nil RollbackErr since nothing completed, got: %v", perr.RollbackErr)
	}
}