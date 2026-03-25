package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/model/converter"
	"github.com/riakgu/moxy/internal/usecase"
)

type SlotController struct {
	UseCase *usecase.SlotUseCase
	Log     *logrus.Logger
}

func NewSlotController(useCase *usecase.SlotUseCase, log *logrus.Logger) *SlotController {
	return &SlotController{
		UseCase: useCase,
		Log:     log,
	}
}

func (c *SlotController) List(ctx *fiber.Ctx) error {
	slots := c.UseCase.ListAll()
	responses := make([]model.SlotResponse, 0, len(slots))
	for _, s := range slots {
		responses = append(responses, *converter.SlotToResponse(s))
	}

	return ctx.JSON(model.WebResponse[[]model.SlotResponse]{
		Data: responses,
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
		if errors.Is(err, model.ErrSlotNotFound) {
			return fiber.NewError(fiber.StatusNotFound, err.Error())
		}
		if errors.Is(err, model.ErrSlotBusy) {
			return fiber.NewError(fiber.StatusConflict, err.Error())
		}
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(model.WebResponse[*model.SlotResponse]{
		Data: response,
	})
}

func (c *SlotController) Provision(ctx *fiber.Ctx) error {
	request := &model.ProvisionRequest{}
	if err := ctx.BodyParser(request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	if request.Slots <= 0 {
		request.Slots = 20
	}

	response, err := c.UseCase.ProvisionSlots(request.Interface, request.Slots, request.DNS64)
	if err != nil {
		c.Log.WithError(err).Error("provision failed")
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(model.WebResponse[*model.ProvisionResponse]{
		Data: response,
	})
}

func (c *SlotController) Delete(ctx *fiber.Ctx) error {
	slotName := ctx.Params("slotName")

	if err := c.UseCase.DestroySlot(slotName); err != nil {
		if errors.Is(err, model.ErrSlotNotFound) {
			return fiber.NewError(fiber.StatusNotFound, err.Error())
		}
		if errors.Is(err, model.ErrSlotBusy) {
			return fiber.NewError(fiber.StatusConflict, err.Error())
		}
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(model.WebResponse[string]{
		Data: "slot deleted",
	})
}

func (c *SlotController) Teardown(ctx *fiber.Ctx) error {
	response, err := c.UseCase.TeardownAll()
	if err != nil {
		c.Log.WithError(err).Error("teardown failed")
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(model.WebResponse[*model.ProvisionResponse]{
		Data: response,
	})
}
