package http

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/usecase"
)

type DeviceController struct {
	DeviceUC *usecase.DeviceUseCase
	Log      *slog.Logger
}

func NewDeviceController(deviceUC *usecase.DeviceUseCase, log *slog.Logger) *DeviceController {
	return &DeviceController{DeviceUC: deviceUC, Log: log}
}

func (c *DeviceController) ListADB(ctx *fiber.Ctx) error {
	serials, err := c.DeviceUC.ListADBDevices()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if serials == nil {
		serials = []string{}
	}
	return ctx.JSON(model.WebResponse[[]string]{Data: serials})
}

func (c *DeviceController) Scan(ctx *fiber.Ctx) error {
	resp, err := c.DeviceUC.Scan()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return ctx.JSON(model.WebResponse[*model.ScanResponse]{Data: resp})
}

func (c *DeviceController) List(ctx *fiber.Ctx) error {
	devices, err := c.DeviceUC.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if devices == nil {
		devices = []model.DeviceResponse{}
	}
	return ctx.JSON(model.WebResponse[[]model.DeviceResponse]{Data: devices})
}

func (c *DeviceController) Get(ctx *fiber.Ctx) error {
	resp, err := c.DeviceUC.GetByAlias(&model.GetDeviceRequest{Alias: ctx.Params("alias")})
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return ctx.JSON(model.WebResponse[*model.DeviceResponse]{Data: resp})
}

func (c *DeviceController) Delete(ctx *fiber.Ctx) error {
	if err := c.DeviceUC.Delete(&model.DeleteDeviceRequest{Alias: ctx.Params("alias")}); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return ctx.JSON(model.WebResponse[bool]{Data: true})
}

func (c *DeviceController) Reset(ctx *fiber.Ctx) error {
	resp, err := c.DeviceUC.Reset(ctx.UserContext(), &model.DeleteDeviceRequest{Alias: ctx.Params("alias")})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return ctx.JSON(model.WebResponse[*model.SetupResponse]{Data: resp})
}

func (c *DeviceController) Provision(ctx *fiber.Ctx) error {
	req := new(model.ProvisionRequest)
	if err := ctx.BodyParser(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	req.Alias = ctx.Params("alias")

	resp, err := c.DeviceUC.Provision(req)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return ctx.JSON(model.WebResponse[*model.ProvisionResponse]{Data: resp})
}

func (c *DeviceController) Setup(ctx *fiber.Ctx) error {
	resp, err := c.DeviceUC.Setup(ctx.UserContext(), &model.SetupDeviceRequest{Alias: ctx.Params("alias")})
	if err != nil {
		if errors.Is(err, model.ErrDeviceNotDetected) {
			return fiber.NewError(fiber.StatusConflict, err.Error())
		}
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return ctx.JSON(model.WebResponse[*model.SetupResponse]{Data: resp})
}
