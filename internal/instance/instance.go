// Package instance modela el registro local de instancias: cada una con su
// motor, versión, recursos, mundo y ajustes. Una instancia es una carpeta
// <data>/instances/<slug>/ con su instance.yaml y (al arrancar) su compose.yml.
package instance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/manuelserver/badomens/internal/host"
)

// Nombre del archivo de definición de la instancia.
const SpecFile = "instance.yaml"

var (
	slugRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,23}$`)
	validDiff = map[string]bool{"peaceful": true, "easy": true, "normal": true, "hard": true}
	validMode = map[string]bool{"survival": true, "creative": true, "adventure": true, "spectator": true}
)

// Spec es la definición completa de una instancia.
type Spec struct {
	Slug             string            `yaml:"slug" json:"slug"`
	Name             string            `yaml:"name" json:"name"`
	Engine           string            `yaml:"engine" json:"engine"`
	Version          string            `yaml:"version" json:"version"`
	Pins             map[string]string `yaml:"pins" json:"pins"`
	Memory           string            `yaml:"memory" json:"memory"`
	CPU              int               `yaml:"cpu" json:"cpu"`
	Port             int               `yaml:"port" json:"port"`
	Difficulty       string            `yaml:"difficulty" json:"difficulty"`
	Gamemode         string            `yaml:"gamemode" json:"gamemode"`
	MaxPlayers       int               `yaml:"max_players" json:"max_players"`
	OnlineMode       bool              `yaml:"online_mode" json:"online_mode"`
	Motd             string            `yaml:"motd" json:"motd"`
	Icon             string            `yaml:"icon" json:"icon"`
	WhitelistEnabled bool              `yaml:"whitelist_enabled" json:"whitelist_enabled"`
	Whitelist        []string          `yaml:"whitelist" json:"whitelist"`
	Ops              []string          `yaml:"ops" json:"ops"`
	Seed             string            `yaml:"seed" json:"seed"`
	Plugins          []string          `yaml:"plugins" json:"plugins"`
	CreatedAt        string            `yaml:"created_at" json:"created_at"`
}

// NormalizeSlug convierte un nombre en un slug válido y lo valida.
func NormalizeSlug(name string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.NewReplacer(
		" ", "-", "_", "-", "á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ñ", "n",
	).Replace(s)
	if !slugRe.MatchString(s) {
		return "", fmt.Errorf("slug inválido %q (usa 2-24 caracteres a-z0-9 y guiones)", name)
	}
	return s, nil
}

// Validate comprueba la coherencia de la definición.
func (s *Spec) Validate(engines []string) error {
	if s.Slug == "" {
		return errors.New("slug vacío")
	}
	found := false
	for _, e := range engines {
		if e == s.Engine {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("motor desconocido %q", s.Engine)
	}
	if s.Version == "" {
		return errors.New("versión vacía")
	}
	if host.ParseMemory(s.Memory) == 0 {
		return fmt.Errorf("memoria inválida %q (ej. 4G)", s.Memory)
	}
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("puerto inválido %d", s.Port)
	}
	if s.Difficulty != "" && !validDiff[s.Difficulty] {
		return fmt.Errorf("dificultad inválida %q", s.Difficulty)
	}
	if s.Gamemode != "" && !validMode[s.Gamemode] {
		return fmt.Errorf("modo inválido %q", s.Gamemode)
	}
	if s.MaxPlayers < 1 || s.MaxPlayers > 1000 {
		return fmt.Errorf("max_players inválido %d", s.MaxPlayers)
	}
	return nil
}

// Manager opera sobre el registro de instancias en dataDir.
type Manager struct {
	DataDir string
	engines []string
}

// New crea un Manager.
func New(dataDir string, engines []string) *Manager {
	return &Manager{DataDir: dataDir, engines: engines}
}

// EngineNames devuelve los nombres de motores conocidos.
func (m *Manager) EngineNames() []string { return m.engines }

// Dir devuelve la carpeta de una instancia.
func (m *Manager) Dir(slug string) string { return filepath.Join(m.DataDir, "instances", slug) }

// SpecPath devuelve la ruta al instance.yaml.
func (m *Manager) SpecPath(slug string) string { return filepath.Join(m.Dir(slug), SpecFile) }

// Load lee una instancia.
func (m *Manager) Load(slug string) (*Spec, error) {
	raw, err := os.ReadFile(m.SpecPath(slug))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("la instancia %q no existe", slug)
		}
		return nil, err
	}
	var s Spec
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("instancia %q ilegible: %w", slug, err)
	}
	return &s, nil
}

// Save escribe la definición de la instancia.
func (m *Manager) Save(s *Spec) error {
	if err := os.MkdirAll(m.Dir(s.Slug), 0o700); err != nil {
		return err
	}
	raw, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(m.SpecPath(s.Slug), raw, 0o600)
}

// Remove borra la carpeta de la instancia (¡no el contenedor!).
func (m *Manager) Remove(slug string) error {
	dir := m.Dir(slug)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("la instancia %q no existe", slug)
	}
	return os.RemoveAll(dir)
}

// List devuelve las instancias ordenadas por slug.
func (m *Manager) List() ([]*Spec, error) {
	root := filepath.Join(m.DataDir, "instances")
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []*Spec{}, nil
		}
		return nil, err
	}
	var out []*Spec
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s, err := m.Load(e.Name())
		if err != nil {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

// NextPort devuelve el primer puerto libre en la franja base (25565+).
func (m *Manager) NextPort(baseStart int) (int, error) {
	specs, err := m.List()
	if err != nil {
		return 0, err
	}
	used := map[int]bool{}
	for _, s := range specs {
		if s.Port > 0 {
			used[s.Port] = true
		}
	}
	for p := baseStart; p < baseStart+100; p++ {
		if !used[p] {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no hay puertos libres a partir de %d", baseStart)
}

// Stamp rellena la marca de creación (time.Now UTC RFC3339).
func Stamp() string { return time.Now().UTC().Format(time.RFC3339) }
