package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/usecase"
)

type UserController struct {
	UseCase *usecase.UserUseCase
	Log     *logrus.Logger
}

func NewUserController(useCase *usecase.UserUseCase, log *logrus.Logger) *UserController {
	return &UserController{
		UseCase: useCase,
		Log:     log,
	}
}

func (c *UserController) List(ctx *fiber.Ctx) error {
	users, err := c.UseCase.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(model.WebResponse[[]model.UserResponse]{
		Data: users,
	})
}

func (c *UserController) Get(ctx *fiber.Ctx) error {
	username := ctx.Params("username")

	user, err := c.UseCase.GetByUsername(username)
	if err != nil {
		if errors.Is(err, model.ErrUserNotFound) {
			return fiber.NewError(fiber.StatusNotFound, err.Error())
		}
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(model.WebResponse[*model.UserResponse]{
		Data: user,
	})
}

func (c *UserController) Create(ctx *fiber.Ctx) error {
	request := &model.CreateUserRequest{}
	if err := ctx.BodyParser(request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	if request.Username == "" || request.Password == "" {
		return fiber.NewError(fiber.StatusBadRequest, "username and password are required")
	}

	user, err := c.UseCase.Create(request)
	if err != nil {
		if errors.Is(err, model.ErrUserAlreadyExists) {
			return fiber.NewError(fiber.StatusConflict, err.Error())
		}
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return ctx.Status(fiber.StatusCreated).JSON(model.WebResponse[*model.UserResponse]{
		Data: user,
	})
}

func (c *UserController) Update(ctx *fiber.Ctx) error {
	request := &model.UpdateUserRequest{
		Username: ctx.Params("username"),
	}
	if err := ctx.BodyParser(request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	user, err := c.UseCase.Update(request)
	if err != nil {
		if errors.Is(err, model.ErrUserNotFound) {
			return fiber.NewError(fiber.StatusNotFound, err.Error())
		}
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(model.WebResponse[*model.UserResponse]{
		Data: user,
	})
}

func (c *UserController) Delete(ctx *fiber.Ctx) error {
	username := ctx.Params("username")

	if err := c.UseCase.Delete(username); err != nil {
		if errors.Is(err, model.ErrUserNotFound) {
			return fiber.NewError(fiber.StatusNotFound, err.Error())
		}
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(model.WebResponse[string]{
		Data: "user deleted",
	})
}
