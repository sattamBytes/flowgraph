// Package workflow is a minimal stub of go.temporal.io/sdk/workflow.
package workflow

import (
	"time"

	"go.temporal.io/sdk/temporal"
)

// Context is the workflow context. Its package path + name ("workflow.Context")
// is how the analyzer recognizes a workflow function's first parameter.
type Context interface {
	Deadline() (time.Time, bool)
}

// ActivityOptions — the analyzer reads timeout/retry presence from this literal.
type ActivityOptions struct {
	TaskQueue              string
	StartToCloseTimeout    time.Duration
	ScheduleToCloseTimeout time.Duration
	ScheduleToStartTimeout time.Duration
	HeartbeatTimeout       time.Duration
	RetryPolicy            *temporal.RetryPolicy
}

// LocalActivityOptions for local activities.
type LocalActivityOptions struct {
	StartToCloseTimeout time.Duration
	RetryPolicy         *temporal.RetryPolicy
}

// ChildWorkflowOptions — the analyzer reads TaskQueue for child workflows.
type ChildWorkflowOptions struct {
	WorkflowID string
	TaskQueue  string
}

// RegisterOptions carries a custom registered Name.
type RegisterOptions struct {
	Name string
}

// Future / channels — stubs.
type Future interface {
	Get(ctx Context, valuePtr interface{}) error
}
type ChildWorkflowFuture interface {
	Future
}
type ReceiveChannel interface {
	Receive(ctx Context, valuePtr interface{}) bool
}

func ExecuteActivity(ctx Context, activity interface{}, args ...interface{}) Future { return nil }
func ExecuteLocalActivity(ctx Context, activity interface{}, args ...interface{}) Future {
	return nil
}
func ExecuteChildWorkflow(ctx Context, childWorkflow interface{}, args ...interface{}) ChildWorkflowFuture {
	return nil
}
func SignalExternalWorkflow(ctx Context, workflowID, runID, signalName string, arg interface{}) Future {
	return nil
}
func GetSignalChannel(ctx Context, signalName string) ReceiveChannel        { return nil }
func SetQueryHandler(ctx Context, queryType string, handler interface{}) error { return nil }
func WithActivityOptions(ctx Context, options ActivityOptions) Context       { return ctx }
func WithLocalActivityOptions(ctx Context, options LocalActivityOptions) Context { return ctx }
func WithChildOptions(ctx Context, options ChildWorkflowOptions) Context     { return ctx }
func Now(ctx Context) time.Time                                             { return time.Time{} }
func Go(ctx Context, f func(ctx Context))                                   {}
