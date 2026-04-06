package http

import (
	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/usecase"
)

// DNSController handles DNS-related API endpoints.
type DNSController struct {
	UseCase *usecase.DNSUseCase
	Log     *logrus.Logger
}

// NewDNSController creates a new DNSController.
func NewDNSController(useCase *usecase.DNSUseCase, log *logrus.Logger) *DNSController {
	return &DNSController{
		UseCase: useCase,
		Log:     log,
	}
}

// Stats returns DNS cache statistics.
// GET /api/dns/stats
func (c *DNSController) Stats(ctx *fiber.Ctx) error {
	stats := c.UseCase.GetCacheStats()

	return ctx.JSON(model.WebResponse[*model.DNSCacheStatsResponse]{
		Data: stats,
	})
}
