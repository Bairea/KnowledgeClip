package config

import (
	"chat-aggregator/internal/models"
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type GlobalConfig struct {
	FormatPrompt   string `yaml:"format_prompt"`
	DefaultTimeout int    `yaml:"default_timeout"`
	MaxConcurrent  int    `yaml:"max_concurrent"`
}

type EngineConfig struct {
	Primary   string            `yaml:"primary"`
	Selectors map[string]string `yaml:"selectors"`
}

type SiteConfig struct {
	ID           string       `yaml:"id"`
	Name         string       `yaml:"name"`
	URL          string       `yaml:"url"`
	Enabled      bool         `yaml:"enabled"`
	Engine       EngineConfig `yaml:"engine"`
	FormatPrompt string       `yaml:"format_prompt"`
	CookieFile   string       `yaml:"cookie_file"`
}

type Config struct {
	Global GlobalConfig `yaml:"global"`
	Sites  []SiteConfig `yaml:"sites"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config yaml: %w", err)
	}

	return &cfg, nil
}

func Save(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}

func (cfg *Config) ToModels() []models.Site {
	result := make([]models.Site, 0, len(cfg.Sites))
	for _, s := range cfg.Sites {
		selectorsJSON, _ := json.Marshal(s.Engine.Selectors)
		site := models.Site{
			ID:           s.ID,
			Name:         s.Name,
			URL:          s.URL,
			EngineType:   s.Engine.Primary,
			Selectors:    string(selectorsJSON),
			CookieFile:   s.CookieFile,
			Enabled:      s.Enabled,
			FormatPrompt: s.FormatPrompt,
		}
		result = append(result, site)
	}
	return result
}
