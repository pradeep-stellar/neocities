package neocities

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type storedConfig struct {
	APIKey   string      `json:"API_KEY"`
	Sitename string      `json:"SITENAME,omitempty"`
	LastPull *storedPull `json:"LAST_PULL,omitempty"`
}

type storedPull struct {
	Time string `json:"time"`
	Loc  string `json:"loc"`
}

func (a *app) configDir() (string, error) {
	goos := a.goos
	if goos == "" {
		goos = runtime.GOOS
	}
	switch goos {
	case "linux":
		if xdg := a.getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, "neocities"), nil
		}
		home, err := a.home()
		if err != nil || home == "" {
			return "", errors.New("home directory is not set")
		}
		return filepath.Join(home, ".config", "neocities"), nil
	case "darwin":
		home, err := a.home()
		if err != nil || home == "" {
			return "", errors.New("home directory is not set")
		}
		return filepath.Join(home, "Library", "Application Support", "neocities"), nil
	default:
		if local := a.getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "neocities"), nil
		}
		if profile := a.getenv("USERPROFILE"); profile != "" {
			return filepath.Join(profile, "Local Settings", "Application Data", "neocities"), nil
		}
		home, err := a.home()
		if err != nil || home == "" {
			return "", errors.New("home directory is not set")
		}
		return filepath.Join(home, ".neocities"), nil
	}
}

func (a *app) configPath() (string, error) {
	dir, err := a.configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func (a *app) loadConfig() (*storedConfig, error) {
	path, err := a.configPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &storedConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg storedConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	cfg.APIKey = trimSpace(cfg.APIKey)
	return &cfg, nil
}

func (a *app) saveConfig(cfg *storedConfig) error {
	path, err := a.configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o600)
}

func trimSpace(s string) string {
	i := 0
	j := len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\n' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}
