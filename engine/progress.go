package engine

import "github.com/gburgyan/aat/plan"

// ProgressObserver receives real-time progress events during plan execution.
// All methods are called synchronously from the engine's execution goroutine.
// Implementations must not block for extended periods.
type ProgressObserver interface {
	// OnRunStart is called once at the beginning of execution with the total
	// number of steps and the effective execution mode.
	OnRunStart(total int, mode string)

	// OnStepStart is called immediately before a step begins execution.
	OnStepStart(index, total int, step plan.Step)

	// OnStepComplete is called after a step finishes (success or failure).
	OnStepComplete(index, total int, result StepResult)

	// OnCleanupStart is called before cleanup execution begins.
	OnCleanupStart(total int)

	// OnCleanupStepComplete is called after each cleanup step finishes.
	OnCleanupStepComplete(index, total int, result StepResult)

	// OnRunComplete is called once at the end of execution with the final result.
	// This fires on every exit path, including early errors.
	OnRunComplete(result *RunResult)
}
