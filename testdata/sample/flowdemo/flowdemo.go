// Package flowdemo exercises the general call-graph engine: a resolved call to a
// first-party helper, an interface call (impl unknown), branch context, and a
// bridge into a Temporal workflow start — all in one function.
package flowdemo

import (
	"context"

	"go.temporal.io/sdk/client"

	wf "example.com/sample/workflows"
)

// Auditor is an interface — calls through it cannot be resolved statically.
type Auditor interface {
	Audit(event string) error
}

// Handle is a plain (non-Temporal) entry function used to test flow:
//   - Handle -> Audit   : interface call, unresolved, under `if orderID == ""`
//   - Handle -> prepare : resolved call to first-party code, no branch guard
//   - Handle -> OrderWorkflow : STARTS_WORKFLOW on the correct queue (clean)
func Handle(ctx context.Context, c client.Client, a Auditor, orderID string) error {
	if orderID == "" {
		_ = a.Audit("empty-order") // interface call inside an if-branch
		return nil
	}
	prepare(orderID)
	_, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{TaskQueue: "orders"}, wf.OrderWorkflow, orderID)
	return err
}

func prepare(id string) { _ = id }
