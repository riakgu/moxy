package config

import (
	"log/slog"
	"github.com/spf13/viper"
)

func NewViper() *viper.Viper {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("json")
	v.AddConfigPath(".")
	v.SetEnvPrefix("MOXY")
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		slog.Warn("config file not found, using defaults and env vars", "error", err)
	}

	return v
}
