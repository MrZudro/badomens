// Package config carga la configuración global de badomens con precedencia
// flags > variables de entorno > archivo. El archivo vive en el directorio de
// configuración del usuario (~/.config/badomens/config.yaml en Linux/macOS,
// %AppData%\badomens\config.yaml en Windows).
package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Variables de entorno reconocidas.
const (
	EnvDataDir      = "BADOMENS_DATA_DIR"
	EnvImage        = "BADOMENS_IMAGE"
	EnvMaxParallel  = "BADOMENS_MAX_PARALLEL"
	EnvMaxInstances = "BADOMENS_MAX_INSTANCES"
)

// Tailscale agrupa la configuración de red privada.
type Tailscale struct {
	// Enabled indica si las instancias se publican solo en la IP del tailnet.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// HostIP es la IP IPv4 efectiva del tailnet. Vacío = se detecta al arrancar.
	HostIP string `yaml:"host_ip" json:"host_ip"`
}

// Config es la configuración global de badomens.
type Config struct {
	// DataDir es la raíz donde viven las instancias, mundos y backups.
	DataDir string `yaml:"data_dir"`
	// Image es el tag de la imagen itzg/minecraft-server.
	Image string `yaml:"image"`
	// MaxInstances es el número máximo de instancias registradas (por defecto 5).
	MaxInstances int `yaml:"max_instances"`
	// MaxParallel es el número máximo de instancias corriendo a la vez (por defecto 3).
	MaxParallel int `yaml:"max_parallel"`
	// DefaultMemory es la RAM JVM por defecto al crear una instancia.
	DefaultMemory string `yaml:"default_memory"`
	// Tailscale agrupa la configuración de red.
	Tailscale Tailscale `yaml:"tailscale"`
	// Repo es el slug owner/repo usado por update --check.
	Repo string `yaml:"repo"`
}

// Default devuelve la configuración con valores por defecto.
func Default() *Config {
	return &Config{
		Image:         "itzg/minecraft-server:java25",
		MaxInstances:  5,
		MaxParallel:   3,
		DefaultMemory: "4G",
		Tailscale:     Tailscale{Enabled: true},
		Repo:          "MrZudro/badomens",
	}
}

// Dir devuelve el directorio de configuración de badomens, creándolo si hace
// falta.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "badomens")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// Path devuelve la ruta del archivo de configuración.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// Load lee la configuración, aplica valores por defecto y variables de entorno.
func Load() (*Config, error) {
	cfg := Default()

	path, err := Path()
	if err != nil {
		return nil, err
	}
	raw, rerr := os.ReadFile(path) // #nosec G304 -- la ruta del config la elige el usuario (config.Path)
	switch {
	case rerr == nil:
		if err := yaml.Unmarshal(raw, cfg); err != nil {
			return nil, err
		}
	case errors.Is(rerr, fs.ErrNotExist):
		// Sin archivo: se usan los valores por defecto.
	default:
		return nil, rerr
	}

	applyEnv(cfg)
	cfg.normalize()
	return cfg, nil
}

// Save escribe la configuración en el archivo del usuario.
func (c *Config) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	raw, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func applyEnv(c *Config) {
	if v := os.Getenv(EnvDataDir); v != "" {
		c.DataDir = v
	}
	if v := os.Getenv(EnvImage); v != "" {
		c.Image = v
	}
	if v := os.Getenv(EnvMaxParallel); v != "" {
		if n, err := parsePositiveInt(v); err == nil {
			c.MaxParallel = n
		}
	}
	if v := os.Getenv(EnvMaxInstances); v != "" {
		if n, err := parsePositiveInt(v); err == nil {
			c.MaxInstances = n
		}
	}
}

func (c *Config) normalize() {
	if c.DataDir == "" {
		dir, err := Dir()
		if err == nil {
			c.DataDir = filepath.Join(dir, "data")
		}
	}
	if c.MaxInstances <= 0 {
		c.MaxInstances = 5
	}
	if c.MaxParallel <= 0 {
		c.MaxParallel = 3
	}
	if c.MaxParallel > c.MaxInstances {
		c.MaxParallel = c.MaxInstances
	}
	if c.Image == "" {
		c.Image = Default().Image
	}
	if c.DefaultMemory == "" {
		c.DefaultMemory = "4G"
	}
	if c.Repo == "" {
		c.Repo = Default().Repo
	}
}

// DataDir devuelve la raíz de datos, creándola si hace falta.
func (c *Config) EnsureDataDir() (string, error) {
	if err := os.MkdirAll(c.DataDir, 0o700); err != nil {
		return "", err
	}
	return c.DataDir, nil
}
