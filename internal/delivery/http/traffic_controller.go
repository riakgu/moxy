package http

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/usecase"
)

// TrafficController handles traffic stats API endpoints.
type TrafficController struct {
	UseCase *usecase.TrafficUseCase
	Log     *slog.Logger
}

// NewTrafficController creates a new TrafficController.
func NewTrafficController(useCase *usecase.TrafficUseCase, log *slog.Logger) *TrafficController {
	return &TrafficController{
		UseCase: useCase,
		Log:     log,
	}
}

// List returns all traffic stats.
// GET /api/traffic
func (c *TrafficController) List(ctx *fiber.Ctx) error {
	stats := c.UseCase.List()

	return ctx.JSON(model.WebResponse[*model.TrafficListResponse]{
		Data: stats,
	})
}
