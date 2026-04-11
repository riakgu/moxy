package http

import (
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/riakgu/moxy/internal/model"
)

type ConfigController struct {
	Log        *slog.Logger
	ConfigPath string
}

func NewConfigController(log *slog.Logger, configPath string) *ConfigController {
	return &ConfigController{Log: log, ConfigPath: configPath}
}

// Get reads config.json from disk and returns it as JSON.
func (c *ConfigController) Get(ctx *fiber.Ctx) error {
	data, err := os.ReadFile(c.ConfigPath)
	if err != nil {
		c.Log.Error("failed to read config file", "error", err)
		return fiber.NewError(fiber.StatusInternalServerError, "failed to read config file")
	}

	var raw json.RawMessage = data
	return ctx.JSON(model.WebResponse[json.RawMessage]{Data: raw})
}

// Update validates and saves config to disk atomically.
func (c *ConfigController) Update(ctx *fiber.Ctx) error {
	var cfg model.MoxyConfig
	if err := ctx.BodyParser(&cfg); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON: "+err.Error())
	}

	if errs := cfg.Validate(); errs != nil {
		return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"errors": errs})
	}

	// Marshal with indentation to keep config.json human-readable
	data, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		c.Log.Error("failed to marshal config", "error", err)
		return fiber.NewError(fiber.StatusInternalServerError, "failed to marshal config")
	}

	// Atomic write: temp file → rename
	tmpPath := c.ConfigPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		c.Log.Error("failed to write temp config", "error", err)
		return fiber.NewError(fiber.StatusInternalServerError, "failed to write config")
	}
	if err := os.Rename(tmpPath, c.ConfigPath); err != nil {
		c.Log.Error("failed to rename config", "error", err)
		_ = os.Remove(tmpPath)
		return fiber.NewError(fiber.StatusInternalServerError, "failed to save config")
	}

	c.Log.Info("config updated via dashboard")
	return ctx.JSON(model.WebResponse[json.RawMessage]{Data: data})
}

// Restart triggers a systemd service restart.
func (c *ConfigController) Restart(ctx *fiber.Ctx) error {
	c.Log.Warn("service restart requested via dashboard")

	go func() {
		// Give HTTP response time to flush
		time.Sleep(500 * time.Millisecond)

		cmd := exec.Command("systemctl", "restart", "moxy")
		if err := cmd.Run(); err != nil {
			c.Log.Error("restart failed", "error", err)
		}
	}()

	return ctx.JSON(model.WebResponse[string]{Data: "restarting"})
}
