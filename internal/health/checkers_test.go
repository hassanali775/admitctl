package health

import (
	"context"
	"strings"
	"testing"

	"github.com/hassanli775/admitctl/internal/tenant"
)

func TestConfigValidationChecker_ValidConfigIsHealthy(t *testing.T) {
	rec := sampleRecord("acme")
	res := ConfigValidationChecker{}.Check(context.Background(), rec)

	if res.Status != StatusHealthy {
		t.Fatalf("expected healthy, got %s (%s)", res.Status, res.Message)
	}
}

func TestConfigValidationChecker_InvalidConfigIsUnhealthy(t *testing.T) {
	rec := sampleRecord("acme")
	rec.Config.Auth = tenant.AuthMethod("not-a-real-method")

	res := ConfigValidationChecker{}.Check(context.Background(), rec)

	if res.Status != StatusUnhealthy {
		t.Fatalf("expected unhealthy, got %s", res.Status)
	}
	if !strings.Contains(res.Message, "auth") {
		t.Fatalf("expected message to mention the failing field, got: %q", res.Message)
	}
}

func TestSchemaVersionChecker_SupportedIsHealthy(t *testing.T) {
	checker := NewSchemaVersionChecker([]string{"v1", "v2"}, []string{"v0.9"})
	rec := sampleRecord("acme")
	rec.Config.DataSchemaVersion = "v1"

	res := checker.Check(context.Background(), rec)

	if res.Status != StatusHealthy {
		t.Fatalf("expected healthy for supported version, got %s", res.Status)
	}
}

func TestSchemaVersionChecker_DeprecatedIsDegraded(t *testing.T) {
	checker := NewSchemaVersionChecker([]string{"v1", "v2"}, []string{"v0.9"})
	rec := sampleRecord("acme")
	rec.Config.DataSchemaVersion = "v0.9"

	res := checker.Check(context.Background(), rec)

	if res.Status != StatusDegraded {
		t.Fatalf("expected degraded for deprecated version, got %s", res.Status)
	}
}

func TestSchemaVersionChecker_UnknownIsUnhealthy(t *testing.T) {
	checker := NewSchemaVersionChecker([]string{"v1", "v2"}, []string{"v0.9"})
	rec := sampleRecord("acme")
	rec.Config.DataSchemaVersion = "v99"

	res := checker.Check(context.Background(), rec)

	if res.Status != StatusUnhealthy {
		t.Fatalf("expected unhealthy for unrecognized version, got %s", res.Status)
	}
}

func TestRateLimitHeadroomChecker_BurstAboveRPSIsHealthy(t *testing.T) {
	rec := sampleRecord("acme")
	rec.Config.RateLimit = tenant.RateLimit{RequestsPerSecond: 100, Burst: 200}

	res := RateLimitHeadroomChecker{}.Check(context.Background(), rec)

	if res.Status != StatusHealthy {
		t.Fatalf("expected healthy when burst exceeds rps, got %s", res.Status)
	}
}

func TestRateLimitHeadroomChecker_BurstEqualRPSIsDegraded(t *testing.T) {
	rec := sampleRecord("acme")
	rec.Config.RateLimit = tenant.RateLimit{RequestsPerSecond: 100, Burst: 100}

	res := RateLimitHeadroomChecker{}.Check(context.Background(), rec)

	if res.Status != StatusDegraded {
		t.Fatalf("expected degraded when burst equals rps (no headroom), got %s", res.Status)
	}
}

// Sanity check that the concrete checkers compose correctly through
// a real Runner, not just in isolation.
func TestRunner_WithRealCheckers(t *testing.T) {
	runner := NewRunner(
		ConfigValidationChecker{},
		NewSchemaVersionChecker([]string{"v1"}, nil),
		RateLimitHeadroomChecker{},
	)

	healthy := sampleRecord("acme")
	healthy.Config.RateLimit = tenant.RateLimit{RequestsPerSecond: 10, Burst: 20}
	if report := runner.RunOne(context.Background(), healthy); report.Overall != StatusHealthy {
		t.Fatalf("expected fully healthy tenant to report healthy, got %s (%+v)", report.Overall, report.Results)
	}

	noHeadroom := sampleRecord("globex")
	noHeadroom.Config.RateLimit = tenant.RateLimit{RequestsPerSecond: 10, Burst: 10}
	if report := runner.RunOne(context.Background(), noHeadroom); report.Overall != StatusDegraded {
		t.Fatalf("expected no-headroom tenant to report degraded, got %s (%+v)", report.Overall, report.Results)
	}

	badSchema := sampleRecord("initech")
	badSchema.Config.DataSchemaVersion = "v99"
	if report := runner.RunOne(context.Background(), badSchema); report.Overall != StatusUnhealthy {
		t.Fatalf("expected unrecognized-schema tenant to report unhealthy, got %s (%+v)", report.Overall, report.Results)
	}
}