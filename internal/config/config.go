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
	"strings"

	"gopkg.in/yaml.v3"
)

// Variables de entorno reconocidas.
const (
	EnvDataDir       = "BADOMENS_DATA_DIR"
	EnvImage         = "BADOMENS_IMAGE"
	EnvMaxParallel   = "BADOMENS_MAX_PARALLEL"
	EnvMaxInstances  = "BADOMENS_MAX_INSTANCES"
	EnvUpdateCheck   = "BADOMENS_UPDATE_CHECK"
	EnvTheme         = "BADOMENS_THEME"
	EnvReducedMotion = "BADOMENS_REDUCED_MOTION"
	EnvASCII         = "BADOMENS_ASCII"
	EnvPortsRange    = "BADOMENS_PORTS_RANGE"
	EnvTailnetScan   = "BADOMENS_TAILNET_SCAN"
)

// UI agrupa la configuración de la interfaz (tema, animaciones, iconos).
type UI struct {
	// Theme es el tema de color: dark, light, high-contrast o colorblind.
	Theme string `yaml:"theme" json:"theme"`
	// ReducedMotion desactiva las animaciones (pulso, transiciones, spinners).
	ReducedMotion bool `yaml:"reduced_motion" json:"reduced_motion"`
	// ASCII usa iconos y bordes solo ASCII (sin emoji/Unicode).
	ASCII bool `yaml:"ascii" json:"ascii"`
}

// Ports agrupa la configuración de asignación de puertos (RF-3).
type Ports struct {
	// Range es el rango de puertos, formato "25565-25664".
	Range string `yaml:"range" json:"range"`
	// TailnetScan decide el sondeo de peers: off | warn | block.
	TailnetScan string `yaml:"tailnet_scan" json:"tailnet_scan"`
}

// Docker agrupa la configuración de reparación de Docker (RF-6).
type Docker struct {
	// EnsureTimeout es el tiempo máximo de espera al reparar ("120s").
	EnsureTimeout string `yaml:"ensure_timeout" json:"ensure_timeout"`
}

// Versions agrupa la caché del catálogo de versiones (RF-5).
type Versions struct {
	// CacheTTL es el tiempo de vida de la caché ("6h").
	CacheTTL string `yaml:"cache_ttl" json:"cache_ttl"`
}

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
	// UI agrupa la configuración de la interfaz.
	UI UI `yaml:"ui"`
	// Ports agrupa la asignación de puertos.
	Ports Ports `yaml:"ports"`
	// Docker agrupa la reparación de Docker.
	Docker Docker `yaml:"docker"`
	// Versions agrupa la caché de versiones.
	Versions Versions `yaml:"versions"`
	// Repo es el slug owner/repo usado por update --check.
	Repo string `yaml:"repo"`
	// UpdateCheck comprueba si hay una versión nueva al abrir la TUI
	// (interactivo) y, si la hay, pregunta si actualizar.
	UpdateCheck bool `yaml:"update_check"`
}

// Default devuelve la configuración con valores por defecto.
func Default() *Config {
	return &Config{
		Image:         "itzg/minecraft-server:java25",
		MaxInstances:  5,
		MaxParallel:   3,
		DefaultMemory: "4G",
		Tailscale:     Tailscale{Enabled: true},
		UI:            UI{Theme: "dark"},
		Ports:         Ports{Range: "25565-25664", TailnetScan: "warn"},
		Docker:        Docker{EnsureTimeout: "120s"},
		Versions:      Versions{CacheTTL: "6h"},
		Repo:          "MrZudro/badomens",
		UpdateCheck:   true,
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
	if v := os.Getenv(EnvUpdateCheck); v != "" {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "0", "no", "false", "off":
			c.UpdateCheck = false
		default:
			c.UpdateCheck = true
		}
	}
	if v := os.Getenv(EnvTheme); v != "" {
		c.UI.Theme = v
	}
	if v := os.Getenv(EnvReducedMotion); v != "" {
		c.UI.ReducedMotion = parseBool(v)
	}
	if v := os.Getenv(EnvASCII); v != "" {
		c.UI.ASCII = parseBool(v)
	}
	if v := os.Getenv(EnvPortsRange); v != "" {
		c.Ports.Range = v
	}
	if v := os.Getenv(EnvTailnetScan); v != "" {
		c.Ports.TailnetScan = v
	}
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "yes", "true", "on", "si":
		return true
	}
	return false
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
	if c.UI.Theme == "" {
		c.UI.Theme = "dark"
	}
	if c.Ports.Range == "" {
		c.Ports.Range = "25565-25664"
	}
	switch strings.ToLower(c.Ports.TailnetScan) {
	case "", "off", "warn", "block":
	default:
		c.Ports.TailnetScan = "warn"
	}
	if c.Docker.EnsureTimeout == "" {
		c.Docker.EnsureTimeout = "120s"
	}
	if c.Versions.CacheTTL == "" {
		c.Versions.CacheTTL = "6h"
	}
}

// DataDir devuelve la raíz de datos, creándola si hace falta.
func (c *Config) EnsureDataDir() (string, error) {
	if err := os.MkdirAll(c.DataDir, 0o700); err != nil {
		return "", err
	}
	return c.DataDir, nil
}
