package onboarding

import (
	"context"
	"errors"
	"fmt"

	"github.com/hassanli775/admitctl/internal/health"
	"github.com/hassanli775/admitctl/internal/registry"
	"github.com/hassanli775/admitctl/internal/store"
	"github.com/hassanli775/admitctl/internal/tenant"
)

// RegisterStep adds a tenant to the registry. Its compensating
// action hard-deletes the tenant via Registry.Remove — not
// Deactivate — since a rolled-back onboarding attempt must leave no
// trace, not a deactivated phantom record.
type RegisterStep struct {
	Reg    *registry.Registry
	Config tenant.Config
}

func (s *RegisterStep) Name() string { return "register" }

func (s *RegisterStep) Do(_ context.Context) error {
	return s.Reg.Register(s.Config)
}

func (s *RegisterStep) Undo(_ context.Context) error {
	return s.Reg.Remove(s.Config.ID)
}

// UnhealthyConfigError is returned by HealthCheckStep when a tenant's
// initial health report comes back Unhealthy. A Degraded report is
// not fatal to onboarding — only Unhealthy blocks it outright.
type UnhealthyConfigError struct {
	Report health.Report
}

func (e *UnhealthyConfigError) Error() string {
	return fmt.Sprintf("tenant %q failed its initial health check (status: %s)", e.Report.TenantID, e.Report.Overall)
}

// HealthCheckStep runs the platform's standard health checks against
// a freshly registered tenant and rejects onboarding if the result
// is Unhealthy. It performs no state mutation of its own — it only
// reads the registry — so Undo is a no-op; there's nothing for it to
// compensate for.
type HealthCheckStep struct {
	Reg      *registry.Registry
	TenantID string
	Runner   *health.Runner

	// Report is populated once Do runs, so callers (e.g. the CLI)
	// can inspect the full check results after Pipeline.Run returns
	// — including a Degraded report on the success path.
	Report health.Report
}

func (s *HealthCheckStep) Name() string { return "initial-health-check" }

func (s *HealthCheckStep) Do(ctx context.Context) error {
	rec, err := s.Reg.Get(s.TenantID)
	if err != nil {
		return err
	}
	s.Report = s.Runner.RunOne(ctx, rec)
	if s.Report.Overall == health.StatusUnhealthy {
		return &UnhealthyConfigError{Report: s.Report}
	}
	return nil
}

func (s *HealthCheckStep) Undo(_ context.Context) error { return nil }

// PersistStep writes the registry's current state to the JSON store.
// Its Undo restores the store file to whatever it contained
// immediately before Do ran — a snapshot captured at Do time, not
// re-derived from the (possibly already rolled-back) in-memory
// registry. That makes Undo correct no matter what order other
// steps' Undo calls happen to run in.
type PersistStep struct {
	StorePath string
	Reg       *registry.Registry

	previous []registry.Record
}

func (s *PersistStep) Name() string { return "persist" }

func (s *PersistStep) Do(_ context.Context) error {
	prev, err := store.Load(s.StorePath)
	switch {
	case err == nil:
		s.previous = prev
	case errors.Is(err, store.ErrNotExist):
		s.previous = nil
	default:
		return err
	}
	return store.Save(s.StorePath, s.Reg.List())
}

func (s *PersistStep) Undo(_ context.Context) error {
	return store.Save(s.StorePath, s.previous)
}