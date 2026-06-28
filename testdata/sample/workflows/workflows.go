// Package workflows holds the worker-side workflow and activity code.
package workflows

import (
	"context"
	"net/http"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

var labels = map[string]string{}

// OrderWorkflow is the "clean" workflow: activities run with a timeout and a
// retry policy, and it listens for the "CancelOrder" signal.
func OrderWorkflow(ctx workflow.Context, orderID string) error {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	workflow.ExecuteActivity(ctx, ChargeCard, orderID)
	workflow.ExecuteActivity(ctx, SendEmail, orderID)

	// Correct signal listener — matches a properly-named sender.
	_ = workflow.GetSignalChannel(ctx, "CancelOrder")
	return nil
}

// ShippingWorkflow has planted bugs: a nondeterministic time.Now() call, and an
// activity executed with NO timeout/retry options.
func ShippingWorkflow(ctx workflow.Context, orderID string) error {
	deadline := time.Now() // BUG (non-determinism): use workflow.Now(ctx)
	_ = deadline

	for k := range labels { // BUG (non-determinism): map range order is random
		_ = k
	}

	go func() { _ = orderID }() // BUG (non-determinism): use workflow.Go

	resp, _ := http.Get("http://shipping.internal") // BUG (non-determinism): network I/O in a workflow
	_ = resp

	// BUG (missing-timeout / missing-retry): no workflow.WithActivityOptions.
	workflow.ExecuteActivity(ctx, GenerateLabel, orderID)
	return nil
}

// ---- activities ----

func ChargeCard(ctx context.Context, orderID string) error    { return nil }
func SendEmail(ctx context.Context, orderID string) error     { return nil }
func GenerateLabel(ctx context.Context, orderID string) error { return nil }

// RefundCard is registered by the worker but never executed — an ORPHAN.
func RefundCard(ctx context.Context, orderID string) error { return nil }
