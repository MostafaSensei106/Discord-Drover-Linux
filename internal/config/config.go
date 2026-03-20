package config

import (
	"fmt"

	"gopkg.in/ini.v1"
)

const DiscordPath = "/usr/bin/discord"

type config struct {
	ProxyURL    string
	DirectMode  bool
	NFQueueNum  uint16
	DiscordPath string
	DiscordArgs []string

	UDPfragmentation bool
	FakeTTL          uint8
}

func Load(path string) (*config, error) {
	cfg := &config{
		NFQueueNum:       1,
		UDPfragmentation: true,
		FakeTTL:          5,
		DiscordPath:      DiscordPath,
	}

	if path == "" {
		return cfg, nil
	}

	iniCfg, err := ini.Load(path)
	if err != nil {
		return nil, fmt.Errorf("Error loading Discord Drover config: %s", err)
	}

	section := iniCfg.Section("bypass")
	cfg.ProxyURL = section.Key("proxy_url").String()
	cfg.DirectMode = section.Key("direct_mode").MustBool(false)
	cfg.DiscordPath = section.Key("discord_path").MustString(DiscordPath)
	cfg.UDPfragmentation = section.Key("udp_fragmentation").MustBool(true)
	cfg.FakeTTL = uint8(section.Key("fake_ttl").MustUint(5))

	return cfg, nil
}
