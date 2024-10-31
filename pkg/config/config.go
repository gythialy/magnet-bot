package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/rs/zerolog"

	"github.com/gythialy/magnet/pkg/constant"
)

type PDFServiceConfig struct {
	WebhookServer     string
	WebhookServerPort int
	PDFServiceURL     string
}

func (c *PDFServiceConfig) Init() *PDFServiceConfig {
	if u := os.Getenv(constant.PDFServerUrl); u != "" {
		c.PDFServiceURL = u
	}
	if u := os.Getenv(constant.WebhookServerURL); u != "" {
		c.WebhookServer = u
	}

	if v := os.Getenv(constant.WebhookServerPort); v != "" {
		c.WebhookServerPort, _ = strconv.Atoi(v)
	}
	return c
}

func (c *PDFServiceConfig) WebhookURL() string {
	return fmt.Sprintf("%s:%d", c.WebhookServer, c.WebhookServerPort)
}

type ServiceConfig struct {
	PDF              *PDFServiceConfig
	ManagerId        int64
	MessageServerUrl string
	BaseDir          string
	LogLevel         zerolog.Level
}

func NewServiceConfig() *ServiceConfig {
	pdf := &PDFServiceConfig{}
	return &ServiceConfig{
		PDF:              pdf.Init(),
		ManagerId:        ManagerId(),
		MessageServerUrl: MessageServerUrl(),
		BaseDir:          BaseDir(),
		LogLevel:         LogLevel(),
	}
}
