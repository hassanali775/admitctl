// Package onboarding sequences the steps involved in bringing a new
// tenant online — register, initial health check, persist — as a
// Pipeline of compensable Steps. If any step fails, every step that
// already completed is undone in reverse order, so a failed
// onboarding attempt leaves the system exactly as it was before the
// attempt started. This is the same compensating-transaction (saga)
// pattern real deployment systems use for rollback-safe releases.
package onboarding

import (
	"context"
	"errors"
	"fmt"
)

// Step is one compensable unit of work in an onboarding pipeline.
// Undo must reverse whatever Do did, and must be safe to call even
// if Do never ran for a later step in the same pipeline — Undo is
// only ever invoked for steps whose Do already succeeded.
type Step interface {
	Name() string
	Do(ctx context.Context) error
	Undo(ctx context.Context) error
}

// Pipeline runs a fixed, ordered sequence of Steps.
type Pipeline struct {
	steps []Step
}

// NewPipeline builds a Pipeline that runs steps in the given order.
func NewPipeline(steps ...Step) *Pipeline {
	return &Pipeline{steps: steps}
}

// PipelineError is returned by Run when a step fails. Cause is the
// original error from the failing step's Do. RollbackErr is nil if
// every already-completed step's Undo succeeded; otherwise it
// aggregates whatever went wrong while rolling back, which callers
// should treat as a serious condition — the system may be left in a
// partially-undone state and needs operator attention.
type PipelineError struct {
	FailedStep  string
	Cause       error
	RollbackErr error
}

func (e *PipelineError) Error() string {
	if e.RollbackErr != nil {
		return fmt.Sprintf("onboarding failed at step %q: %v (rollback ALSO failed: %v)", e.FailedStep, e.Cause, e.RollbackErr)
	}
	return fmt.Sprintf("onboarding failed at step %q: %v (rolled back cleanly)", e.FailedStep, e.Cause)
}

// Unwrap exposes Cause so callers can errors.As/errors.Is straight
// through a PipelineError to the underlying failure (e.g. a
// tenant.ValidationErrors or a health check failure).
func (e *PipelineError) Unwrap() error { return e.Cause }

// Run executes each step in order. On the first failure, it undoes
// every previously completed step in reverse order and returns a
// *PipelineError describing what failed and how rollback went. Run
// returns nil only if every step succeeded.
func (p *Pipeline) Run(ctx context.Context) error {
	completed := make([]Step, 0, len(p.steps))

	for _, step := range p.steps {
		if err := step.Do(ctx); err != nil {
			return &PipelineError{
				FailedStep:  step.Name(),
				Cause:       err,
				RollbackErr: undoAll(ctx, completed),
			}
		}
		completed = append(completed, step)
	}

	return nil
}

// undoAll calls Undo on each step in reverse completion order,
// running every Undo even if an earlier one fails, and aggregates
// any failures into a single error (nil if all succeeded).
func undoAll(ctx context.Context, completed []Step) error {
	var errs []error
	for i := len(completed) - 1; i >= 0; i-- {
		step := completed[i]
		if err := step.Undo(ctx); err != nil {
			errs = append(errs, fmt.Errorf("undo %q: %w", step.Name(), err))
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}