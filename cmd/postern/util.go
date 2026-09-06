package main

import (
	"fmt"
	"os"
	"os/user"
	"strings"

	"github.com/neitomic/postern/internal/agent"
	"github.com/neitomic/postern/internal/config"
	"github.com/neitomic/postern/internal/names"
)

func loadConfigured() (config.Client, agent.Paths, error) {
	p, err := agent.DefaultPaths()
	if err != nil {
		return config.Client{}, p, err
	}
	cfg, err := config.LoadClient(p.ConfigFile())
	if err != nil {
		if os.IsNotExist(err) {
			return config.Client{}, p, fmt.Errorf("client config not found (run postern config set server …): %s", p.ConfigFile())
		}
		return config.Client{}, p, err
	}
	return cfg, p, nil
}

func defaultLoginUser() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	name := strings.ToLower(strings.TrimSpace(u.Username))
	if names.ValidLoginUser(name) != nil {
		return ""
	}
	return name
}
