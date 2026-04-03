package http

import (
	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/usecase"
)

type DeviceController struct {
	DeviceUC *usecase.DeviceUseCase
	SlotUC   *usecase.SlotUseCase
	Log      *logrus.Logger
}

func NewDeviceController(deviceUC *usecase.DeviceUseCase, slotUC *usecase.SlotUseCase, log *logrus.Logger) *DeviceController {
	return &DeviceController{DeviceUC: deviceUC, SlotUC: slotUC, Log: log}
}

func (c *DeviceController) ListADB(ctx *fiber.Ctx) error {
	serials, err := c.DeviceUC.ListADBDevices()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if serials == nil {
		serials = []string{}
	}
	return ctx.JSON(fiber.Map{"data": serials})
}

func (c *DeviceController) Register(ctx *fiber.Ctx) error {
	req := new(model.RegisterDeviceRequest)
	if err := ctx.BodyParser(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	resp, err := c.DeviceUC.Register(req)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return ctx.Status(fiber.StatusCreated).JSON(fiber.Map{"data": resp})
}

func (c *DeviceController) List(ctx *fiber.Ctx) error {
	devices, err := c.DeviceUC.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if devices == nil {
		devices = []model.DeviceResponse{}
	}
	return ctx.JSON(fiber.Map{"data": devices})
}

func (c *DeviceController) Get(ctx *fiber.Ctx) error {
	resp, err := c.DeviceUC.GetByID(ctx.Params("deviceId"))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return ctx.JSON(fiber.Map{"data": resp})
}

func (c *DeviceController) Setup(ctx *fiber.Ctx) error {
	req := &model.SetupDeviceRequest{DeviceId: ctx.Params("deviceId")}
	progress, err := c.DeviceUC.Setup(req)
	if err != nil {
		return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"data": progress})
	}
	return ctx.JSON(fiber.Map{"data": progress})
}

func (c *DeviceController) Teardown(ctx *fiber.Ctx) error {
	if err := c.DeviceUC.Teardown(ctx.Params("deviceId")); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return ctx.JSON(fiber.Map{"data": true})
}

func (c *DeviceController) Delete(ctx *fiber.Ctx) error {
	if err := c.DeviceUC.Delete(ctx.Params("deviceId")); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return ctx.JSON(fiber.Map{"data": true})
}

func (c *DeviceController) UpdateOverride(ctx *fiber.Ctx) error {
	req := new(model.UpdateISPOverrideRequest)
	if err := ctx.BodyParser(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	req.DeviceId = ctx.Params("deviceId")
	resp, err := c.DeviceUC.UpdateISPOverride(req)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return ctx.JSON(fiber.Map{"data": resp})
}

func (c *DeviceController) Provision(ctx *fiber.Ctx) error {
	deviceId := ctx.Params("deviceId")
	device, err := c.DeviceUC.GetByID(deviceId)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}

	var body struct {
		Slots int `json:"slots"`
	}
	if err := ctx.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if body.Slots <= 0 {
		body.Slots = 5
	}

	resp, err := c.SlotUC.ProvisionSlots(device.Alias, device.Interface, body.Slots, device.Nameserver, device.NAT64Prefix)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return ctx.JSON(fiber.Map{"data": resp})
}

