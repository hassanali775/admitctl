package onboarding

import (
	"context"
	"errors"
	"testing"
)

// fakeStep logs every Do/Undo call (in "do:name" / "undo:name" form)
// into a shared log slice, so tests can assert both which steps ran
// and in what order — the thing that actually matters for rollback
// correctness.
type fakeStep struct {
	name    string
	doErr   error
	undoErr error
	log     *[]string
}

func (f *fakeStep) Name() string { return f.name }

func (f *fakeStep) Do(_ context.Context) error {
	*f.log = append(*f.log, "do:"+f.name)
	return f.doErr
}

func (f *fakeStep) Undo(_ context.Context) error {
	*f.log = append(*f.log, "undo:"+f.name)
	return f.undoErr
}

func TestPipeline_AllStepsSucceed(t *testing.T) {
	var log []string
	p := NewPipeline(
		&fakeStep{name: "a", log: &log},
		&fakeStep{name: "b", log: &log},
		&fakeStep{name: "c", log: &log},
	)

	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	want := []string{"do:a", "do:b", "do:c"}
	assertLog(t, log, want)
}

func TestPipeline_MiddleStepFails_RollsBackOnlyCompletedSteps(t *testing.T) {
	var log []string
	boom := errors.New("boom")
	p := NewPipeline(
		&fakeStep{name: "a", log: &log},
		&fakeStep{name: "b", doErr: boom, log: &log},
		&fakeStep{name: "c", log: &log}, // must never run
	)

	err := p.Run(context.Background())
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	var perr *PipelineError
	if !errors.As(err, &perr) {
		t.Fatalf("expected *PipelineError, got %T", err)
	}
	if perr.FailedStep != "b" {
		t.Fatalf("expected failed step %q, got %q", "b", perr.FailedStep)
	}
	if !errors.Is(perr.Cause, boom) {
		t.Fatalf("expected Cause to be the original error, got %v", perr.Cause)
	}
	if perr.RollbackErr != nil {
		t.Fatalf("expected clean rollback, got: %v", perr.RollbackErr)
	}

	// "c" never ran (its Do never fired), and "a" was undone —
	// exactly once, after "b" failed.
	want := []string{"do:a", "do:b", "undo:a"}
	assertLog(t, log, want)
}

func TestPipeline_FirstStepFails_NothingToRollBack(t *testing.T) {
	var log []string
	boom := errors.New("boom")
	p := NewPipeline(
		&fakeStep{name: "a", doErr: boom, log: &log},
		&fakeStep{name: "b", log: &log},
	)

	err := p.Run(context.Background())
	var perr *PipelineError
	if !errors.As(err, &perr) {
		t.Fatalf("expected *PipelineError, got %T", err)
	}
	if perr.RollbackErr != nil {
		t.Fatalf("expected nil RollbackErr when nothing completed, got: %v", perr.RollbackErr)
	}

	want := []string{"do:a"}
	assertLog(t, log, want)
}

func TestPipeline_RollbackRunsInReverseOrder(t *testing.T) {
	var log []string
	boom := errors.New("boom")
	p := NewPipeline(
		&fakeStep{name: "a", log: &log},
		&fakeStep{name: "b", log: &log},
		&fakeStep{name: "c", log: &log},
		&fakeStep{name: "d", doErr: boom, log: &log},
	)

	if err := p.Run(context.Background()); err == nil {
		t.Fatal("expected an error")
	}

	want := []string{"do:a", "do:b", "do:c", "do:d", "undo:c", "undo:b", "undo:a"}
	assertLog(t, log, want)
}

func TestPipeline_RollbackFailure_StillAttemptsEveryUndo(t *testing.T) {
	var log []string
	boom := errors.New("boom")
	undoBoom := errors.New("undo boom")
	p := NewPipeline(
		&fakeStep{name: "a", log: &log},
		&fakeStep{name: "b", undoErr: undoBoom, log: &log},
		&fakeStep{name: "c", doErr: boom, log: &log},
	)

	err := p.Run(context.Background())
	var perr *PipelineError
	if !errors.As(err, &perr) {
		t.Fatalf("expected *PipelineError, got %T", err)
	}
	if perr.RollbackErr == nil {
		t.Fatal("expected a non-nil RollbackErr when an Undo call fails")
	}
	if !errors.Is(perr.RollbackErr, undoBoom) {
		t.Fatalf("expected RollbackErr to wrap the undo failure, got: %v", perr.RollbackErr)
	}

	// Both "b" and "a" must have had Undo attempted, even though
	// "b"'s Undo failed — a failed compensating action must not
	// stop the rest of the rollback from running.
	want := []string{"do:a", "do:b", "do:c", "undo:b", "undo:a"}
	assertLog(t, log, want)
}

func assertLog(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected log %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected log %v, got %v (mismatch at index %d)", want, got, i)
		}
	}
}