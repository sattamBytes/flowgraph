// Command worker registers workflows and activities on task queues.
//
// OrderWorkflow is registered on the "orders" queue — but the control plane
// starts it on "payments" (see ../control), which is the planted task-queue
// mismatch.
package main

import (
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	wf "example.com/sample/workflows"
)

func main() {
	c, _ := client.Dial(client.Options{})
	defer c.Close()

	// "orders" queue.
	w := worker.New(c, "orders", worker.Options{})
	w.RegisterWorkflow(wf.OrderWorkflow)
	w.RegisterActivity(wf.ChargeCard)
	w.RegisterActivity(wf.SendEmail)
	w.RegisterActivity(wf.RefundCard) // registered, never started -> orphan

	// "shipping" queue. ShippingWorkflow is registered under a custom name to
	// exercise RegisterWorkflowWithOptions name mapping.
	s := worker.New(c, "shipping", worker.Options{})
	s.RegisterWorkflowWithOptions(wf.ShippingWorkflow, workflow.RegisterOptions{Name: "ship.v1"})
	s.RegisterActivity(wf.GenerateLabel)

	_ = w.Start()
	_ = s.Start()
}
