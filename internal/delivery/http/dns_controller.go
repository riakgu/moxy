package http

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/usecase"
)

type DNSController struct {
	UseCase *usecase.DNSUseCase
	Log     *slog.Logger
}

func NewDNSController(useCase *usecase.DNSUseCase, log *slog.Logger) *DNSController {
	return &DNSController{
		UseCase: useCase,
		Log:     log,
	}
}

func (c *DNSController) Stats(ctx *fiber.Ctx) error {
	stats := c.UseCase.GetCacheStats()

	return ctx.JSON(model.WebResponse[*model.DNSCacheStatsResponse]{
		Data: stats,
	})
}
