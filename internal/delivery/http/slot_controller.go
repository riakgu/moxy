package http

import (
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
