package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}
	v := viper.New()
	v.SetConfigFile(path)
	v.SetEnvPrefix("BACKUPD")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	raw := v.AllSettings()
	interpolateEnv(raw)

	var cfg Config
	if err := decode(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

func interpolateEnv(v any) {
	switch val := v.(type) {
	case map[string]any:
		for k, child := range val {
			if s, ok := child.(string); ok {
				val[k] = expandEnv(s)
			} else {
				interpolateEnv(child)
			}
		}
	case []any:
		for i, item := range val {
			if s, ok := item.(string); ok {
				val[i] = expandEnv(s)
			} else {
				interpolateEnv(item)
			}
		}
	}
}

func DefaultConfigPath() string {
	if p := os.Getenv("BACKUPD_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "/etc/backupd.yaml"
	}
	return home + "/.backupd.yaml"
}

func expandEnv(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		start := strings.Index(s[i:], "${")
		if start == -1 {
			result.WriteString(s[i:])
			break
		}
		result.WriteString(s[i : i+start])
		end := strings.Index(s[i+start:], "}")
		if end == -1 {
			result.WriteString(s[i+start:])
			break
		}
		key := s[i+start+2 : i+start+end]
		val := os.Getenv(key)
		result.WriteString(val)
		i += start + end + 1
	}
	return result.String()
}
