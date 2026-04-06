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
	App               *fiber.App
	DeviceController  *httpdelivery.DeviceController
	SlotController    *httpdelivery.SlotController
	DNSController     *httpdelivery.DNSController
	TrafficController *httpdelivery.TrafficController
	Log               *logrus.Logger
	StaticFS          embed.FS
}

func (c *RouteConfig) Setup() {
	api := c.App.Group("/api")

	// Device routes — static routes BEFORE :alias wildcard
	api.Get("/devices/adb", c.DeviceController.ListADB)
	api.Post("/devices/scan", c.DeviceController.Scan)
	api.Get("/devices", c.DeviceController.List)
	api.Get("/devices/:alias", c.DeviceController.Get)
	api.Delete("/devices/:alias", c.DeviceController.Delete)
	api.Post("/devices/:alias/provision", c.DeviceController.Provision)
	api.Post("/devices/:alias/setup", c.DeviceController.Setup)

	// Slot routes — static routes BEFORE :slotName wildcard
	api.Post("/slots/cleanup", c.SlotController.Cleanup)
	api.Get("/slots", c.SlotController.List)
	api.Get("/slots/:slotName", c.SlotController.Get)
	api.Post("/slots/:slotName/changeip", c.SlotController.ChangeIP)
	api.Delete("/slots/:slotName", c.SlotController.Delete)

	// DNS routes
	api.Get("/dns/stats", c.DNSController.Stats)

	// Traffic routes
	api.Get("/traffic", c.TrafficController.List)

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
