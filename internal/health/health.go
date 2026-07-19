// Package health defines a pluggable health-check subsystem for
// tenants. A Checker evaluates one narrow aspect of a tenant's
// config or state; a Runner executes a set of Checkers against one
// or many tenants and aggregates the results into a Report.
//
// Checkers are deliberately synchronous, dependency-free functions
// of a registry.Record — this is a simulator, so "health" here means
// "is this tenant's configuration and state sound," not "did a
// network call to a real backend succeed." That keeps checks fast,
// deterministic, and easy to test, and mirrors how an FDE would
// validate a client's deployment config before it ever touches
// production traffic.
package health

import (
	"context"
	"time"

	"github.com/hassanli775/admitctl/internal/registry"
)

// Status is the outcome of a single check or an aggregated report.
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusDegraded  Status = "degraded"
	StatusUnhealthy Status = "unhealthy"
)

// severity ranks Status by how bad it is; higher is worse. Used to
// compute a Report's Overall status as the worst of its Results.
func (s Status) severity() int {
	switch s {
	case StatusUnhealthy:
		return 2
	case StatusDegraded:
		return 1
	default:
		return 0
	}
}

// worse returns whichever of a, b is the more severe status.
func worse(a, b Status) Status {
	if b.severity() > a.severity() {
		return b
	}
	return a
}

// CheckResult is the outcome of one Checker run against one tenant.
type CheckResult struct {
	Name    string
	Status  Status
	Message string
}

// Checker evaluates one aspect of a tenant's health. Implementations
// must be safe to reuse across many tenants and should not mutate
// the Record they're given.
type Checker interface {
	Name() string
	Check(ctx context.Context, rec registry.Record) CheckResult
}

// Report aggregates every Checker's result for a single tenant.
// Overall is the worst status among Results (or StatusHealthy if
// Results is empty).
type Report struct {
	TenantID string
	Overall  Status
	Results  []CheckResult
	RanAt    time.Time
}

// Runner executes a fixed set of Checkers against tenant records.
type Runner struct {
	checkers []Checker
}

// NewRunner builds a Runner that will run every given checker, in
// order, against each tenant it's asked to evaluate.
func NewRunner(checkers ...Checker) *Runner {
	return &Runner{checkers: checkers}
}

// RunOne runs every configured checker against rec and returns the
// aggregated Report.
func (r *Runner) RunOne(ctx context.Context, rec registry.Record) Report {
	results := make([]CheckResult, 0, len(r.checkers))
	overall := StatusHealthy

	for _, c := range r.checkers {
		res := c.Check(ctx, rec)
		if res.Name == "" {
			res.Name = c.Name()
		}
		results = append(results, res)
		overall = worse(overall, res.Status)
	}

	return Report{
		TenantID: rec.Config.ID,
		Overall:  overall,
		Results:  results,
		RanAt:    time.Now(),
	}
}

// RunMany runs RunOne against every record and returns one Report per
// tenant, in the same order as records.
func (r *Runner) RunMany(ctx context.Context, records []registry.Record) []Report {
	reports := make([]Report, 0, len(records))
	for _, rec := range records {
		reports = append(reports, r.RunOne(ctx, rec))
	}
	return reports
}