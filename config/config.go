package config

import (
	"fmt"

	"github.com/spf13/viper"
)

type Config struct {
	AppPort    string `mapstructure:"APP_PORT"`
	APIID      int    `mapstructure:"TELEGRAM_API_ID"`
	APIHash    string `mapstructure:"TELEGRAM_API_HASH"`
	SessionDir string `mapstructure:"SESSION_DIR"`
}

func LoadConfig() (*Config, error) {

	v := viper.New()
	v.SetConfigName(".env")
	v.SetConfigType("env")

	v.AddConfigPath(".")

	v.SetDefault("APP_PORT", "50051")

	v.AutomaticEnv()

	var config Config
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return nil, fmt.Errorf("config file not found")
		}
		return nil, fmt.Errorf("error reading config.yml: %w", err)
	}

	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("error parsing : %w", err)
	}

	return &config, nil
}
