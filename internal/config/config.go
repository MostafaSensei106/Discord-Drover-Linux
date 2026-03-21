package config

import (
	"fmt"

	"github.com/MostafaSensei106/Discord-Drover-Linux/internal/constants"
	"gopkg.in/ini.v1"
)

type Config struct {
	ProxyURL    string
	DirectMode  bool
	NFQueueNum  uint16
	DiscordPath string
	DiscordArgs []string

	UDPFragmentation bool
	FakeTTL          uint8
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		NFQueueNum:       1,
		UDPFragmentation: true,
		FakeTTL:          5,
		DiscordPath:      constants.DiscordPath,
	}

	if path == "" {
		return cfg, nil
	}

	iniCfg, err := ini.Load(path)
	if err != nil {
		return nil, fmt.Errorf("Error loading Discord Drover config: %s", err)
	}

	section := iniCfg.Section("bypass")
	cfg.ProxyURL = section.Key("proxy").String()
	cfg.DirectMode = section.Key("direct_mode").MustBool(false)
	cfg.DiscordPath = section.Key("discord_path").MustString(constants.DiscordPath)
	cfg.UDPFragmentation = section.Key("udp_fragmentation").MustBool(true)
	cfg.FakeTTL = uint8(section.Key("fake_ttl").MustUint(5))

	return cfg, nil
}
