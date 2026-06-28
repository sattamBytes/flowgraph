// Command control is the API server / control plane. It starts workflows BY
// NAME and BY function reference, and contains several planted bugs.
package main

import (
	"context"
	"os"

	"go.temporal.io/sdk/client"

	wf "example.com/sample/workflows"
)

func StartOrder(ctx context.Context, c client.Client, orderID string) error {
	// BUG (task-queue-mismatch): OrderWorkflow is registered on "orders" but
	// started here on "payments" — no worker polls "payments", so it hangs.
	_, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{TaskQueue: "payments"}, wf.OrderWorkflow, orderID)
	return err
}

func StartOrderByName(ctx context.Context, c client.Client, orderID string) error {
	// BUG (unknown-name): "OrderWorkfow" is a typo — never registered.
	_, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{TaskQueue: "orders"}, "OrderWorkfow", orderID)
	return err
}

func StartShipping(ctx context.Context, c client.Client, orderID string) error {
	// CLEAN: registered as "ship.v1" on "shipping", started on "shipping".
	_, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{TaskQueue: "shipping"}, "ship.v1", orderID)
	return err
}

func StartDynamic(ctx context.Context, c client.Client, orderID string) error {
	// UNRESOLVED: workflow name comes from the environment — cannot be resolved
	// statically. The analyzer must mark this edge unresolved, not guess.
	name := os.Getenv("WORKFLOW_NAME")
	_, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{TaskQueue: "orders"}, name, orderID)
	return err
}

func CancelOrder(ctx context.Context, c client.Client, workflowID string) error {
	// BUG (signal-mismatch): the workflow listens for "CancelOrder" (capital C);
	// this sends "cancelOrder" — no handler matches.
	return c.SignalWorkflow(ctx, workflowID, "", "cancelOrder", nil)
}

func main() {
	c, _ := client.Dial(client.Options{})
	defer c.Close()
	ctx := context.Background()
	_ = StartOrder(ctx, c, "o-1")
	_ = StartOrderByName(ctx, c, "o-2")
	_ = StartShipping(ctx, c, "o-3")
	_ = StartDynamic(ctx, c, "o-4")
	_ = CancelOrder(ctx, c, "wf-1")
}
