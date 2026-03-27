package http

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/usecase"
)

type StatsController struct {
	UseCase *usecase.SlotUseCase
	ProxyUC *usecase.ProxyUseCase
	Log     *logrus.Logger
}

func NewStatsController(useCase *usecase.SlotUseCase, proxyUC *usecase.ProxyUseCase, log *logrus.Logger) *StatsController {
	return &StatsController{
		UseCase: useCase,
		ProxyUC: proxyUC,
		Log:     log,
	}
}

func (c *StatsController) Stats(ctx *fiber.Ctx) error {
	stats := c.UseCase.GetStats()
	return ctx.JSON(model.WebResponse[*model.StatsResponse]{
		Data: stats,
	})
}

func (c *StatsController) Health(ctx *fiber.Ctx) error {
	health := c.UseCase.GetHealth()
	return ctx.JSON(model.WebResponse[*model.HealthResponse]{
		Data: health,
	})
}

func (c *StatsController) Destinations(ctx *fiber.Ctx) error {
	limit, _ := strconv.Atoi(ctx.Query("limit", "100"))
	stats := c.ProxyUC.GetDestinationStats(limit)
	return ctx.JSON(model.WebResponse[*model.DestinationStatsResponse]{
		Data: stats,
	})
}
