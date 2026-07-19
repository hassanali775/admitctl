package onboarding

import (
	"github.com/hassanli775/admitctl/internal/health"
	"github.com/hassanli775/admitctl/internal/registry"
	"github.com/hassanli775/admitctl/internal/tenant"
)

// NewOnboardPipeline builds the standard three-step onboarding
// pipeline for cfg: register, initial health check, persist. It also
// returns the HealthCheckStep directly so callers can inspect its
// Report after Run — including on the success path, e.g. to warn
// about a Degraded (but accepted) tenant.
func NewOnboardPipeline(reg *registry.Registry, storePath string, cfg tenant.Config, runner *health.Runner) (*Pipeline, *HealthCheckStep) {
	healthCheck := &HealthCheckStep{Reg: reg, TenantID: cfg.ID, Runner: runner}

	pipeline := NewPipeline(
		&RegisterStep{Reg: reg, Config: cfg},
		healthCheck,
		&PersistStep{StorePath: storePath, Reg: reg},
	)

	return pipeline, healthCheck
}