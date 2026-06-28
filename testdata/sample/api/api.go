// Package api wires REST entrypoints (net/http + chi) to handlers and shows the
// full flow bridging a route into a Temporal workflow:
//
//	POST /orders -> CreateOrderHandler -> startOrder -> STARTS_WORKFLOW OrderWorkflow
package api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-chi/chi/v5"
	"github.com/labstack/echo/v4"
	"go.temporal.io/sdk/client"

	wf "example.com/sample/workflows"
)

var temporalClient client.Client

// Register binds routes via both the stdlib mux and a chi router.
func Register(mux *http.ServeMux, r chi.Router) {
	mux.HandleFunc("POST /orders", CreateOrderHandler)
	r.Get("/orders/{id}", GetOrderHandler)
}

// CreateOrderHandler is a net/http handler that bridges into Temporal.
func CreateOrderHandler(w http.ResponseWriter, req *http.Request) {
	startOrder(req.Context(), "o-rest")
}

// GetOrderHandler is a chi handler with no downstream flow.
func GetOrderHandler(w http.ResponseWriter, req *http.Request) {
	_ = req
}

func startOrder(ctx context.Context, id string) {
	temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{TaskQueue: "orders"}, wf.OrderWorkflow, id)
}

// RegisterGin wires a gin route (variadic handlers — last is the handler).
func RegisterGin(g *gin.Engine) {
	g.POST("/gin/orders", GinCreate)
}

func GinCreate(c *gin.Context) {}

// RegisterEcho wires an echo route (handler is the 2nd arg, middleware follows).
func RegisterEcho(e *echo.Echo) {
	e.GET("/echo/orders", EchoList)
}

func EchoList(c echo.Context) error { return nil }
