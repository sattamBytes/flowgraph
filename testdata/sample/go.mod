module example.com/sample

go 1.23

require (
	github.com/gin-gonic/gin v1.0.0
	github.com/go-chi/chi/v5 v5.0.0
	github.com/labstack/echo/v4 v4.0.0
	go.temporal.io/sdk v1.0.0
)

replace go.temporal.io/sdk => ../stubsdk

replace github.com/go-chi/chi/v5 => ../stubchi

replace github.com/gin-gonic/gin => ../stubgin

replace github.com/labstack/echo/v4 => ../stubecho
