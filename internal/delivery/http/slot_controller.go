package http

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"

	"github.com/riakgu/moxy/internal/delivery/proxy"
	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/usecase"
)

type SlotController struct {
	UseCase     *usecase.SlotUseCase
	Log         *slog.Logger
	PortHandler *proxy.PortBasedHandler
}

func NewSlotController(useCase *usecase.SlotUseCase, log *slog.Logger, portHandler *proxy.PortBasedHandler) *SlotController {
	return &SlotController{
		UseCase:     useCase,
		Log:         log,
		PortHandler: portHandler,
	}
}

func (c *SlotController) List(ctx *fiber.Ctx) error {
	slots := c.UseCase.ListAll()

	return ctx.JSON(model.WebResponse[[]model.SlotResponse]{
		Data: slots,
	})
}

func (c *SlotController) Get(ctx *fiber.Ctx) error {
	request := &model.GetSlotRequest{
		SlotName: ctx.Params("slotName"),
	}

	response, err := c.UseCase.GetByName(request)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}

	return ctx.JSON(model.WebResponse[*model.SlotResponse]{
		Data: response,
	})
}

func (c *SlotController) ChangeIP(ctx *fiber.Ctx) error {
	request := &model.ChangeIPRequest{
		SlotName: ctx.Params("slotName"),
	}

	response, err := c.UseCase.RecycleSlot(request)
	if err != nil {
		if errors.Is(err, entity.ErrSlotNotFound) {
			return fiber.NewError(fiber.StatusNotFound, err.Error())
		}
		if errors.Is(err, entity.ErrSlotBusy) {
			return fiber.NewError(fiber.StatusConflict, err.Error())
		}
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(model.WebResponse[*model.SlotResponse]{
		Data: response,
	})
}

func (c *SlotController) Delete(ctx *fiber.Ctx) error {
	slotName := ctx.Params("slotName")

	if err := c.UseCase.DestroySlot(slotName); err != nil {
		if errors.Is(err, entity.ErrSlotNotFound) {
			return fiber.NewError(fiber.StatusNotFound, err.Error())
		}
		if errors.Is(err, entity.ErrSlotBusy) {
			return fiber.NewError(fiber.StatusConflict, err.Error())
		}
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	if c.PortHandler != nil {
		slotNames := c.UseCase.GetSlotNames()
		c.PortHandler.SyncSlots(slotNames)
		c.PortHandler.SyncSlotsIPv6(slotNames)
	}

	return ctx.JSON(model.WebResponse[string]{
		Data: "slot deleted",
	})
}

func (c *SlotController) Cleanup(ctx *fiber.Ctx) error {
	cleaned, err := c.UseCase.CleanupOrphans()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(model.WebResponse[model.CleanupResponse]{
		Data: model.CleanupResponse{Cleaned: cleaned},
	})
}
