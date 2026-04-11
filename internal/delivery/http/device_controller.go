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

type DeviceController struct {
	DeviceUC     *usecase.DeviceUseCase
	Log          *slog.Logger
	PortHandler  *proxy.PortBasedHandler
	GetSlotNames func() []string
}

func NewDeviceController(deviceUC *usecase.DeviceUseCase, log *slog.Logger, portHandler *proxy.PortBasedHandler, getSlotNames func() []string) *DeviceController {
	return &DeviceController{DeviceUC: deviceUC, Log: log, PortHandler: portHandler, GetSlotNames: getSlotNames}
}

// syncPorts syncs both slot and device proxy ports after mutations.
func (c *DeviceController) syncPorts() {
	if c.PortHandler == nil {
		return
	}
	slotNames := c.GetSlotNames()
	onlineAliases := c.DeviceUC.ListOnlineAliases()
	c.PortHandler.SyncSlots(slotNames)
	c.PortHandler.SyncDevices(onlineAliases)
	c.PortHandler.SyncSlotsIPv6(slotNames)
	c.PortHandler.SyncDevicesIPv6(onlineAliases)
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
	c.syncPorts()
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
	resp, err := c.DeviceUC.GetByAlias(ctx.Params("alias"))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return ctx.JSON(model.WebResponse[*model.DeviceResponse]{Data: resp})
}

func (c *DeviceController) Delete(ctx *fiber.Ctx) error {
	if err := c.DeviceUC.Delete(ctx.Params("alias")); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	c.syncPorts()
	return ctx.JSON(model.WebResponse[bool]{Data: true})
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
	c.syncPorts()
	return ctx.JSON(model.WebResponse[*model.ProvisionResponse]{Data: resp})
}

func (c *DeviceController) Setup(ctx *fiber.Ctx) error {
	alias := ctx.Params("alias")
	resp, err := c.DeviceUC.Setup(ctx.UserContext(), alias)
	if err != nil {
		if errors.Is(err, entity.ErrDeviceNotDetected) {
			return fiber.NewError(fiber.StatusConflict, err.Error())
		}
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	c.syncPorts()
	return ctx.JSON(model.WebResponse[*model.SetupResponse]{Data: resp})
}
