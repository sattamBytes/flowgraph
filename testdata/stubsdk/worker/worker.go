// Package worker is a minimal stub of go.temporal.io/sdk/worker.
package worker

import (
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/workflow"
)

// Options for New.
type Options struct {
	MaxConcurrentActivityExecutionSize int
}

// Worker registers workflows/activities for a task queue.
type Worker interface {
	RegisterWorkflow(w interface{})
	RegisterWorkflowWithOptions(w interface{}, options workflow.RegisterOptions)
	RegisterActivity(a interface{})
	RegisterActivityWithOptions(a interface{}, options activity.RegisterOptions)
	Start() error
	Run(interruptCh <-chan interface{}) error
	Stop()
}

// New creates a worker bound to taskQueue. The analyzer reads taskQueue (the
// 2nd argument) to know which queue each registration belongs to.
func New(c client.Client, taskQueue string, options Options) Worker { return nil }
