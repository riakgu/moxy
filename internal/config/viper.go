package config

import (
	"github.com/sirupsen/logrus"
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
		logrus.Warnf("config file not found, using defaults and env vars: %v", err)
	}

	return v
}
