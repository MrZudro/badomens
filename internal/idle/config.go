// Package idle implementa el auto-apagado por inactividad (RF-8): detecta si hay
// jugadores conectados (Server List Ping, con RCON como respaldo), cuenta el
// tiempo sin nadie y apaga la instancia limpiamente.
//
// La máquina de estados es pura y recibe el reloj por parámetro, de forma que se
// puede probar sin esperas reales. La principio rector es: ante la duda, no
// apagar.
package idle

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config es la configuración de auto-apagado de una instancia.
type Config struct {
	// Enabled activa el auto-apagado.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Timeout es el tiempo sin jugadores antes de apagar.
	Timeout time.Duration `yaml:"timeout" json:"timeout"`
	// FirstJoinGrace es el tiempo tras iniciar para que entre alguien.
	FirstJoinGrace time.Duration `yaml:"first_join_grace" json:"first_join_grace"`
	// WarnBefore es el aviso previo en el chat/la TUI.
	WarnBefore time.Duration `yaml:"warn_before" json:"warn_before"`
	// Action es la acción a ejecutar ("stop"; reservado "pause").
	Action string `yaml:"action" json:"action"`
	// BackupBeforeStop hace un backup antes de apagar.
	BackupBeforeStop bool `yaml:"backup_before_stop" json:"backup_before_stop"`
	// PollInterval es cada cuánto se comprueba la actividad.
	PollInterval time.Duration `yaml:"poll_interval" json:"poll_interval"`
}

// Límites de validación.
const (
	MinTimeout = time.Minute
	MaxTimeout = 24 * time.Hour
)

// DefaultConfig devuelve la configuración por defecto (deshabilitada).
func DefaultConfig() Config {
	return Config{
		Enabled:        false,
		Timeout:        15 * time.Minute,
		FirstJoinGrace: 10 * time.Minute,
		WarnBefore:     time.Minute,
		Action:         "stop",
		PollInterval:   15 * time.Second,
	}
}

// Normalized rellena los valores vacíos con los por defecto y corrige el
// intervalo de sondeo.
func (c Config) Normalized() Config {
	def := DefaultConfig()
	if c.Timeout <= 0 {
		c.Timeout = def.Timeout
	}
	if c.WarnBefore <= 0 {
		c.WarnBefore = def.WarnBefore
	}
	if c.PollInterval <= 0 {
		c.PollInterval = def.PollInterval
	}
	if c.Action == "" {
		c.Action = def.Action
	}
	return c
}

// Validate comprueba los límites (1 min – 24 h) y la acción.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.Timeout < MinTimeout || c.Timeout > MaxTimeout {
		return fmt.Errorf("el tiempo de auto-apagado debe estar entre %s y %s (tienes %s)", MinTimeout, MaxTimeout, c.Timeout)
	}
	if c.FirstJoinGrace < 0 {
		return fmt.Errorf("el periodo de gracia no puede ser negativo")
	}
	if c.WarnBefore < 0 || c.WarnBefore >= c.Timeout {
		return fmt.Errorf("el aviso previo debe ser menor que el tiempo de auto-apagado")
	}
	if c.Action != "" && c.Action != "stop" {
		return fmt.Errorf("acción de auto-apagado no soportada %q (usa stop)", c.Action)
	}
	return nil
}

// Preset es una opción predefinida de tiempo de auto-apagado.
type Preset struct {
	Label   string
	Timeout time.Duration
}

// Presets devuelve las opciones de la UI. Timeout==0 significa "Nunca".
func Presets() []Preset {
	return []Preset{
		{Label: "5 min", Timeout: 5 * time.Minute},
		{Label: "10 min", Timeout: 10 * time.Minute},
		{Label: "15 min", Timeout: 15 * time.Minute},
		{Label: "30 min", Timeout: 30 * time.Minute},
		{Label: "1 h", Timeout: time.Hour},
		{Label: "2 h", Timeout: 2 * time.Hour},
		{Label: "Nunca", Timeout: 0},
	}
}

// ParseDuration interpreta duraciones tipo "45m", "1h", "90s" o "90" (minutos).
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, fmt.Errorf("duración vacía")
	}
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Minute, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("duración inválida %q (ej. 15m, 1h)", s)
	}
	return d, nil
}

// HumanDuration redacta una duración en lenguaje llano.
func HumanDuration(d time.Duration) string {
	switch {
	case d <= 0:
		return "nunca"
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	default:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			if h == 1 {
				return "1 hora"
			}
			return fmt.Sprintf("%d horas", h)
		}
		return fmt.Sprintf("%dh %dmin", h, m)
	}
}

// yamlView es la representación en YAML de Config (duraciones como texto).
type yamlView struct {
	Enabled          bool   `yaml:"enabled" json:"enabled"`
	Timeout          string `yaml:"timeout" json:"timeout"`
	FirstJoinGrace   string `yaml:"first_join_grace" json:"first_join_grace"`
	WarnBefore       string `yaml:"warn_before" json:"warn_before"`
	Action           string `yaml:"action" json:"action"`
	BackupBeforeStop bool   `yaml:"backup_before_stop" json:"backup_before_stop"`
	PollInterval     string `yaml:"poll_interval" json:"poll_interval"`
}

// MarshalYAML serializa la configuración con duraciones legibles ("15m").
func (c Config) MarshalYAML() (any, error) {
	c = c.Normalized()
	return yamlView{
		Enabled:          c.Enabled,
		Timeout:          c.Timeout.String(),
		FirstJoinGrace:   c.FirstJoinGrace.String(),
		WarnBefore:       c.WarnBefore.String(),
		Action:           c.Action,
		BackupBeforeStop: c.BackupBeforeStop,
		PollInterval:     c.PollInterval.String(),
	}, nil
}

// UnmarshalYAML lee la configuración. La ausencia de un valor deja el campo en
// cero (Enabled=false → auto-apagado deshabilitado en instancias antiguas).
func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	var raw yamlView
	if err := node.Decode(&raw); err != nil {
		return err
	}
	c.Enabled = raw.Enabled
	c.Action = raw.Action
	c.BackupBeforeStop = raw.BackupBeforeStop
	var err error
	if raw.Timeout != "" {
		c.Timeout, err = time.ParseDuration(raw.Timeout)
		if err != nil {
			return fmt.Errorf("idle.timeout inválido %q: %w", raw.Timeout, err)
		}
	}
	if raw.FirstJoinGrace != "" {
		c.FirstJoinGrace, err = time.ParseDuration(raw.FirstJoinGrace)
		if err != nil {
			return fmt.Errorf("idle.first_join_grace inválido %q: %w", raw.FirstJoinGrace, err)
		}
	}
	if raw.WarnBefore != "" {
		c.WarnBefore, err = time.ParseDuration(raw.WarnBefore)
		if err != nil {
			return fmt.Errorf("idle.warn_before inválido %q: %w", raw.WarnBefore, err)
		}
	}
	if raw.PollInterval != "" {
		c.PollInterval, err = time.ParseDuration(raw.PollInterval)
		if err != nil {
			return fmt.Errorf("idle.poll_interval inválido %q: %w", raw.PollInterval, err)
		}
	}
	return nil
}
