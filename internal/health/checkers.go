package health

import (
	"context"
	"fmt"

	"github.com/hassanli775/admitctl/internal/registry"
)

// ConfigValidationChecker re-validates a tenant's stored config
// against the same rules enforced at onboarding time. Today there is
// no path for a stored config to become invalid after registration —
// but this check exists so that if one is ever added (an Update
// method, a manual store-file edit, a migration), drift is caught
// immediately instead of surfacing as a mysterious downstream
// failure.
type ConfigValidationChecker struct{}

func (ConfigValidationChecker) Name() string { return "config-validation" }

func (ConfigValidationChecker) Check(_ context.Context, rec registry.Record) CheckResult {
	if err := rec.Config.Validate(); err != nil {
		return CheckResult{Status: StatusUnhealthy, Message: err.Error()}
	}
	return CheckResult{Status: StatusHealthy, Message: "config passes validation"}
}

// SchemaVersionChecker flags tenants pinned to a data schema version
// the platform doesn't fully support. Versions in Deprecated still
// work today but should be migrated soon (Degraded); versions in
// neither set are treated as unknown/broken (Unhealthy) since the
// platform has no guarantee it can serve them correctly.
type SchemaVersionChecker struct {
	Supported  map[string]bool
	Deprecated map[string]bool
}

// NewSchemaVersionChecker builds a SchemaVersionChecker from plain
// slices, which is friendlier to call sites than building the maps
// by hand.
func NewSchemaVersionChecker(supported, deprecated []string) SchemaVersionChecker {
	s := make(map[string]bool, len(supported))
	for _, v := range supported {
		s[v] = true
	}
	d := make(map[string]bool, len(deprecated))
	for _, v := range deprecated {
		d[v] = true
	}
	return SchemaVersionChecker{Supported: s, Deprecated: d}
}

func (c SchemaVersionChecker) Name() string { return "schema-version" }

func (c SchemaVersionChecker) Check(_ context.Context, rec registry.Record) CheckResult {
	v := rec.Config.DataSchemaVersion
	switch {
	case c.Supported[v]:
		return CheckResult{Status: StatusHealthy, Message: fmt.Sprintf("schema %s is fully supported", v)}
	case c.Deprecated[v]:
		return CheckResult{Status: StatusDegraded, Message: fmt.Sprintf("schema %s is deprecated; plan a migration", v)}
	default:
		return CheckResult{Status: StatusUnhealthy, Message: fmt.Sprintf("schema %s is not recognized by this platform", v)}
	}
}

// RateLimitHeadroomChecker flags tenants whose burst capacity gives
// them no room above their sustained rate, meaning any short traffic
// spike gets throttled immediately rather than absorbed.
type RateLimitHeadroomChecker struct{}

func (RateLimitHeadroomChecker) Name() string { return "rate-limit-headroom" }

func (RateLimitHeadroomChecker) Check(_ context.Context, rec registry.Record) CheckResult {
	rl := rec.Config.RateLimit
	if rl.Burst <= rl.RequestsPerSecond {
		return CheckResult{
			Status:  StatusDegraded,
			Message: fmt.Sprintf("burst (%d) gives no headroom over sustained rate (%d req/s)", rl.Burst, rl.RequestsPerSecond),
		}
	}
	return CheckResult{
		Status:  StatusHealthy,
		Message: fmt.Sprintf("burst (%d) provides headroom over sustained rate (%d req/s)", rl.Burst, rl.RequestsPerSecond),
	}
}