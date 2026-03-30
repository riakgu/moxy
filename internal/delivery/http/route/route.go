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
	UserController  *httpdelivery.UserController
	Log             *logrus.Logger
	StaticFS        embed.FS
}

func (c *RouteConfig) Setup() {
	api := c.App.Group("/api")
	api.Get("/slots", c.SlotController.List)
	api.Get("/slots/:slotName", c.SlotController.Get)
	api.Post("/slots/:slotName/changeip", c.SlotController.ChangeIP)
	api.Post("/provision", c.SlotController.Provision)
	api.Post("/teardown", c.SlotController.Teardown)
	api.Delete("/slots/:slotName", c.SlotController.Delete)
	api.Get("/stats", c.StatsController.Stats)
	api.Get("/health", c.StatsController.Health)
	api.Get("/destinations", c.StatsController.Destinations)

	api.Get("/users", c.UserController.List)
	api.Post("/users", c.UserController.Create)
	api.Get("/users/:username", c.UserController.Get)
	api.Put("/users/:username", c.UserController.Update)
	api.Delete("/users/:username", c.UserController.Delete)

	c.App.Use("/", filesystem.New(filesystem.Config{
		Root:       http.FS(c.StaticFS),
		PathPrefix: "dashboard/dist",
		Browse:     false,
		Index:      "index.html",
	}))

	c.App.Get("/*", func(ctx *fiber.Ctx) error {
		return filesystem.SendFile(ctx, http.FS(c.StaticFS), "dashboard/dist/index.html")
	})
}
