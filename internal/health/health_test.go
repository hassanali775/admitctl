package health

import (
	"context"
	"testing"

	"github.com/hassanli775/admitctl/internal/registry"
	"github.com/hassanli775/admitctl/internal/tenant"
)

// fakeChecker lets tests control exactly what status/message a
// checker returns, so Runner's aggregation logic can be tested
// independently of any real checker's business rules.
type fakeChecker struct {
	name   string
	result CheckResult
}

func (f fakeChecker) Name() string { return f.name }

func (f fakeChecker) Check(_ context.Context, _ registry.Record) CheckResult {
	r := f.result
	if r.Name == "" {
		r.Name = f.name
	}
	return r
}

func sampleRecord(id string) registry.Record {
	return registry.Record{
		Config: tenant.Config{
			ID:                id,
			DisplayName:       "Sample " + id,
			Auth:              tenant.AuthAPIKey,
			DataSchemaVersion: "v1",
			RateLimit:         tenant.RateLimit{RequestsPerSecond: 10, Burst: 20},
		},
		Status: registry.StatusActive,
	}
}

func TestRunOne_AllHealthyOverallHealthy(t *testing.T) {
	runner := NewRunner(
		fakeChecker{name: "a", result: CheckResult{Status: StatusHealthy}},
		fakeChecker{name: "b", result: CheckResult{Status: StatusHealthy}},
	)
	report := runner.RunOne(context.Background(), sampleRecord("acme"))

	if report.Overall != StatusHealthy {
		t.Fatalf("expected overall status healthy, got %s", report.Overall)
	}
	if len(report.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(report.Results))
	}
}

func TestRunOne_OverallIsWorstResult(t *testing.T) {
	runner := NewRunner(
		fakeChecker{name: "a", result: CheckResult{Status: StatusHealthy}},
		fakeChecker{name: "b", result: CheckResult{Status: StatusDegraded}},
	)
	report := runner.RunOne(context.Background(), sampleRecord("acme"))

	if report.Overall != StatusDegraded {
		t.Fatalf("expected overall status degraded, got %s", report.Overall)
	}
}

func TestRunOne_UnhealthyOutranksDegraded(t *testing.T) {
	runner := NewRunner(
		fakeChecker{name: "a", result: CheckResult{Status: StatusDegraded}},
		fakeChecker{name: "b", result: CheckResult{Status: StatusUnhealthy}},
		fakeChecker{name: "c", result: CheckResult{Status: StatusHealthy}},
	)
	report := runner.RunOne(context.Background(), sampleRecord("acme"))

	if report.Overall != StatusUnhealthy {
		t.Fatalf("expected overall status unhealthy, got %s", report.Overall)
	}
}

func TestRunOne_NoCheckersIsHealthy(t *testing.T) {
	runner := NewRunner()
	report := runner.RunOne(context.Background(), sampleRecord("acme"))

	if report.Overall != StatusHealthy {
		t.Fatalf("expected vacuous overall status healthy, got %s", report.Overall)
	}
	if len(report.Results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(report.Results))
	}
}

func TestRunOne_ResultNameDefaultsToCheckerName(t *testing.T) {
	runner := NewRunner(fakeChecker{name: "unnamed-result-checker", result: CheckResult{Status: StatusHealthy}})
	report := runner.RunOne(context.Background(), sampleRecord("acme"))

	if report.Results[0].Name != "unnamed-result-checker" {
		t.Fatalf("expected result name to default to checker name, got %q", report.Results[0].Name)
	}
}

func TestRunOne_SetsTenantID(t *testing.T) {
	runner := NewRunner()
	report := runner.RunOne(context.Background(), sampleRecord("globex"))

	if report.TenantID != "globex" {
		t.Fatalf("expected tenant ID globex, got %q", report.TenantID)
	}
}

func TestRunMany_OneReportPerRecord(t *testing.T) {
	runner := NewRunner(fakeChecker{name: "a", result: CheckResult{Status: StatusHealthy}})
	records := []registry.Record{sampleRecord("acme"), sampleRecord("globex"), sampleRecord("initech")}

	reports := runner.RunMany(context.Background(), records)

	if len(reports) != 3 {
		t.Fatalf("expected 3 reports, got %d", len(reports))
	}
	for i, id := range []string{"acme", "globex", "initech"} {
		if reports[i].TenantID != id {
			t.Errorf("report %d: expected tenant ID %q, got %q", i, id, reports[i].TenantID)
		}
	}
}

func TestRunMany_EmptyRecordsReturnsEmptyReports(t *testing.T) {
	runner := NewRunner(fakeChecker{name: "a", result: CheckResult{Status: StatusHealthy}})
	reports := runner.RunMany(context.Background(), nil)

	if len(reports) != 0 {
		t.Fatalf("expected 0 reports, got %d", len(reports))
	}
}