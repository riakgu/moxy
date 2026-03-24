package config

import (
	"github.com/gofiber/fiber/v2"
	"github.com/spf13/viper"
)

func NewFiber(v *viper.Viper) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:      "Moxy",
		ServerHeader: "Moxy",
		ErrorHandler: func(ctx *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}
			return ctx.Status(code).JSON(fiber.Map{
				"errors": err.Error(),
			})
		},
	})

	return app
}
