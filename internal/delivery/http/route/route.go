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
	App              *fiber.App
	DeviceController *httpdelivery.DeviceController
	SlotController   *httpdelivery.SlotController
	Log              *logrus.Logger
	StaticFS         embed.FS
}

func (c *RouteConfig) Setup() {
	api := c.App.Group("/api")

	// Device routes
	api.Get("/adb-devices", c.DeviceController.ListADB)
	api.Post("/devices", c.DeviceController.Register)
	api.Get("/devices", c.DeviceController.List)
	api.Get("/devices/:deviceId", c.DeviceController.Get)
	api.Delete("/devices/:deviceId", c.DeviceController.Delete)
	api.Post("/devices/:deviceId/setup", c.DeviceController.Setup)
	api.Post("/devices/:deviceId/teardown", c.DeviceController.Teardown)
	api.Put("/devices/:deviceId/override", c.DeviceController.UpdateOverride)
	api.Post("/devices/:deviceId/provision", c.DeviceController.Provision)

	// Slot routes
	api.Get("/slots", c.SlotController.List)
	api.Get("/slots/:slotName", c.SlotController.Get)
	api.Post("/slots/:slotName/changeip", c.SlotController.ChangeIP)
	api.Delete("/slots/:slotName", c.SlotController.Delete)

	// Stats routes
	api.Get("/stats", c.SlotController.Stats)
	api.Get("/health", c.SlotController.Health)

	// Static files (dashboard)
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
