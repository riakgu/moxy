package http

import (
	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/usecase"
)

type ProxyUserController struct {
	UseCase *usecase.ProxyUserUseCase
	Log     *logrus.Logger
}

func NewProxyUserController(useCase *usecase.ProxyUserUseCase, log *logrus.Logger) *ProxyUserController {
	return &ProxyUserController{UseCase: useCase, Log: log}
}

func (c *ProxyUserController) List(ctx *fiber.Ctx) error {
	users, err := c.UseCase.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return ctx.JSON(fiber.Map{"data": users})
}

func (c *ProxyUserController) Create(ctx *fiber.Ctx) error {
	req := new(model.CreateProxyUserRequest)
	if err := ctx.BodyParser(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if req.Username == "" || req.Password == "" {
		return fiber.NewError(fiber.StatusBadRequest, "username and password are required")
	}
	resp, err := c.UseCase.Create(req)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return ctx.Status(fiber.StatusCreated).JSON(fiber.Map{"data": resp})
}

func (c *ProxyUserController) Get(ctx *fiber.Ctx) error {
	resp, err := c.UseCase.GetByUsername(ctx.Params("username"))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return ctx.JSON(fiber.Map{"data": resp})
}

func (c *ProxyUserController) Update(ctx *fiber.Ctx) error {
	req := &model.UpdateProxyUserRequest{Username: ctx.Params("username")}
	if err := ctx.BodyParser(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	resp, err := c.UseCase.Update(req)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return ctx.JSON(fiber.Map{"data": resp})
}

func (c *ProxyUserController) Delete(ctx *fiber.Ctx) error {
	if err := c.UseCase.Delete(ctx.Params("username")); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return ctx.JSON(fiber.Map{"data": "user deleted"})
}
