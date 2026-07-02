package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config holds user-persisted CLI settings.
type Config struct {
	Server        string           `json:"server,omitempty"`
	DeploymentDir string           `json:"deployment_dir,omitempty"`
	CurrentUser   CurrentUser      `json:"current_user,omitempty"`
	Workspace     CurrentWorkspace `json:"workspace,omitempty"`
}

// CurrentUser stores the CLI-selected local user.
type CurrentUser struct {
	ID          string `json:"id,omitempty"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Email       string `json:"email,omitempty"`
}

// CurrentWorkspace stores the CLI-selected workspace.
type CurrentWorkspace struct {
	ID   string `json:"id,omitempty"`
	Slug string `json:"slug,omitempty"`
	Name string `json:"name,omitempty"`
}

// ResolveServer returns the effective server URL using the precedence chain:
// flag > SPECGATE_SERVER env > config file > http://localhost:8080.
func ResolveServer(flag string, cfg Config) string {
	if flag != "" {
		return flag
	}
	if env := os.Getenv("SPECGATE_SERVER"); env != "" {
		return env
	}
	if cfg.Server != "" {
		return cfg.Server
	}
	return "http://localhost:8080"
}

// DefaultPath returns os.UserConfigDir()/specgate/config.json.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "specgate", "config.json"), nil
}

// Load reads the config from DefaultPath. Missing file returns empty Config.
func Load() (Config, error) {
	return LoadFrom("")
}

// LoadFrom reads the config from path. Empty path uses DefaultPath.
func LoadFrom(path string) (Config, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return Config{}, err
		}
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	return c, json.Unmarshal(data, &c)
}

// Save writes the config to DefaultPath atomically with mode 0600.
func (c Config) Save() error {
	return c.SaveTo("")
}

// SaveTo writes the config to path atomically with mode 0600.
// Empty path uses DefaultPath.
func (c Config) SaveTo(path string) error {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
