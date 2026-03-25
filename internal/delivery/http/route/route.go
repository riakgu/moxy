package route

import (
	"embed"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/sirupsen/logrus"

	httpdelivery "github.com/riakgu/moxy/internal/delivery/http"
)

type RouteConfig struct {
	App             *fiber.App
	SlotController  *httpdelivery.SlotController
	StatsController *httpdelivery.StatsController
	Log             *logrus.Logger
	StaticFS        embed.FS
}

func (c *RouteConfig) Setup() {
	api := c.App.Group("/api")
	api.Get("/slots", c.SlotController.List)
	api.Get("/slots/:slotName", c.SlotController.Get)
	api.Get("/stats", c.StatsController.Stats)
	api.Get("/health", c.StatsController.Health)

	c.App.Use("/", filesystem.New(filesystem.Config{
		Root:       http.FS(c.StaticFS),
		PathPrefix: "static",
		Browse:     false,
		Index:      "index.html",
	}))
}
