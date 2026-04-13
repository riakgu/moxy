package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/usecase"
)

type ConfigController struct {
	Log      *slog.Logger
	ConfigUC *usecase.ConfigUseCase
}

func NewConfigController(log *slog.Logger, configUC *usecase.ConfigUseCase) *ConfigController {
	return &ConfigController{Log: log, ConfigUC: configUC}
}

func (c *ConfigController) Get(ctx *fiber.Ctx) error {
	data, err := c.ConfigUC.GetConfig()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to read config file")
	}
	return ctx.JSON(model.WebResponse[json.RawMessage]{Data: data})
}

func (c *ConfigController) Update(ctx *fiber.Ctx) error {
	var cfg model.MoxyConfig
	if err := ctx.BodyParser(&cfg); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON: "+err.Error())
	}

	result, err := c.ConfigUC.UpdateConfig(&cfg)
	if err != nil {
		var valErr *usecase.ValidationError
		if errors.As(err, &valErr) {
			return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"errors": valErr.Fields})
		}
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(model.WebResponse[*usecase.ConfigSaveResult]{Data: result})
}

func (c *ConfigController) Restart(ctx *fiber.Ctx) error {
	go func() {
		time.Sleep(500 * time.Millisecond)
		if err := c.ConfigUC.RestartService(); err != nil {
			c.Log.Error("restart failed", "error", err)
		}
	}()

	return ctx.JSON(model.WebResponse[string]{Data: "restarting"})
}
