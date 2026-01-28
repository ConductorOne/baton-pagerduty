package main

import (
	cfg "github.com/conductorone/baton-pagerduty/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/config"
)

func main() {
	config.Generate("pagerduty", cfg.Config)
}
