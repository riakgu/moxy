package http

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/usecase"
)

type ADBRestarter interface {
	EnsureServer() error
}

type NamespaceCleaner interface {
	CleanupOrphans() (int, error)
}

type SystemController struct {
	Log       *slog.Logger
	SystemUC  *usecase.SystemUseCase
	ConfigUC  *usecase.ConfigUseCase
	ADB       ADBRestarter
	NsCleaner NamespaceCleaner
}

func NewSystemController(
	log *slog.Logger,
	systemUC *usecase.SystemUseCase,
	configUC *usecase.ConfigUseCase,
	adb ADBRestarter,
	nsCleaner NamespaceCleaner,
) *SystemController {
	return &SystemController{
		Log:       log,
		SystemUC:  systemUC,
		ConfigUC:  configUC,
		ADB:       adb,
		NsCleaner: nsCleaner,
	}
}

func (c *SystemController) GetStats(ctx *fiber.Ctx) error {
	stats := c.SystemUC.Collect()
	return ctx.JSON(model.WebResponse[*model.SystemStatsResponse]{Data: stats})
}

func (c *SystemController) RestartADB(ctx *fiber.Ctx) error {
	if err := c.ADB.EnsureServer(); err != nil {
		c.Log.Error("adb restart failed", "error", err)
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	c.Log.Info("adb server restarted via dashboard")
	return ctx.JSON(model.WebResponse[string]{Data: "adb restarted"})
}

func (c *SystemController) CleanupNamespaces(ctx *fiber.Ctx) error {
	count, err := c.NsCleaner.CleanupOrphans()
	if err != nil {
		c.Log.Error("namespace cleanup failed", "error", err)
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	c.Log.Info("orphan namespaces cleaned via dashboard", "removed", count)
	return ctx.JSON(model.WebResponse[fiber.Map]{Data: fiber.Map{
		"removed": count,
	}})
}

func (c *SystemController) Restart(ctx *fiber.Ctx) error {
	go func() {
		time.Sleep(500 * time.Millisecond)
		if err := c.ConfigUC.RestartService(); err != nil {
			c.Log.Error("restart failed", "error", err)
		}
	}()
	return ctx.JSON(model.WebResponse[string]{Data: "restarting"})
}
