// Package client is a minimal stub of go.temporal.io/sdk/client.
package client

import "context"

// StartWorkflowOptions is the options literal the analyzer reads TaskQueue from.
type StartWorkflowOptions struct {
	ID        string
	TaskQueue string
	Namespace string
}

// Options for Dial.
type Options struct {
	HostPort  string
	Namespace string
}

// WorkflowRun is the handle returned by ExecuteWorkflow.
type WorkflowRun interface {
	GetID() string
	GetRunID() string
}

// Client is the control-plane client interface.
type Client interface {
	ExecuteWorkflow(ctx context.Context, options StartWorkflowOptions, workflow interface{}, args ...interface{}) (WorkflowRun, error)
	SignalWorkflow(ctx context.Context, workflowID, runID, signalName string, arg interface{}) error
	SignalWithStartWorkflow(ctx context.Context, workflowID, signalName string, signalArg interface{}, options StartWorkflowOptions, workflow interface{}, workflowArgs ...interface{}) (WorkflowRun, error)
	QueryWorkflow(ctx context.Context, workflowID, runID, queryType string, args ...interface{}) (interface{}, error)
	Close()
}

// Dial returns a stub client. The analyzer never calls this.
func Dial(Options) (Client, error) { return nil, nil }
