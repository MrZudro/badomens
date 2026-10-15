package instance

import (
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/host"
)

// ---------------------------------------------------------------------------
// Reproducibilidad (plan 12.5 · spec 6.4)
// ---------------------------------------------------------------------------
//
// El manifiesto es la descripción DECLARATIVA de una instancia: motor, versión
// REAL de Minecraft y los PINS EXACTOS (nunca "latest"), más la configuración
// que define su comportamiento. NO incluye el mundo ni los datos generados.
//
// Recrear una instancia a partir del manifiesto no consulta la red: la
// resolución ya está fijada. Si un pin obligatorio falta o dice "latest", el
// manifiesto se RECHAZA: no existe una descripción reproducible a medias.

// ManifestSchema identifica el esquema del manifiesto reproducible.
const ManifestSchema = "badomens.instance/1"

// Manifest es la descripción reproducible de una instancia.
type Manifest struct {
	// Schema es la versión del esquema del manifiesto.
	Schema string `json:"schema"`
	// Slug es el identificador con el que se exportó.
	Slug string `json:"slug"`
	// Name es el nombre visible.
	Name string `json:"name,omitempty"`
	// Engine es el motor (paper, fabric, forge, neoforge, vanilla).
	Engine string `json:"engine"`
	// Version es la versión REAL de Minecraft resuelta.
	Version string `json:"version"`
	// Pins son los pines exactos por motor (build/loader), claves de .env.
	Pins map[string]string `json:"pins,omitempty"`
	// Java esperado y su ORIGEN (verified, inferred, unknown).
	JavaMajor  int    `json:"java_major,omitempty"`
	JavaStatus string `json:"java_status,omitempty"`
	// Image es la imagen Docker coherente con el Java requerido.
	Image string `json:"image,omitempty"`
	// Recursos y ajustes del servidor.
	Memory           string   `json:"memory"`
	CPU              int      `json:"cpu,omitempty"`
	Port             int      `json:"port,omitempty"`
	Difficulty       string   `json:"difficulty,omitempty"`
	Gamemode         string   `json:"gamemode,omitempty"`
	MaxPlayers       int      `json:"max_players,omitempty"`
	OnlineMode       bool     `json:"online_mode"`
	Motd             string   `json:"motd,omitempty"`
	WhitelistEnabled bool     `json:"whitelist_enabled"`
	Whitelist        []string `json:"whitelist,omitempty"`
	Ops              []string `json:"ops,omitempty"`
	Seed             string   `json:"seed,omitempty"`
	// Plugins instalados (nombres), para reproducir el conjunto declarado.
	Plugins []string `json:"plugins,omitempty"`
	// CreatedAt es la marca de creación del manifiesto (RFC3339 UTC).
	CreatedAt string `json:"created_at,omitempty"`
}

// ManifestFromSpec construye el manifiesto de una instancia. Copia los mapas y
// listas para no compartir memoria con el registro.
func ManifestFromSpec(s Spec) Manifest {
	return Manifest{
		Schema:           ManifestSchema,
		Slug:             s.Slug,
		Name:             s.Name,
		Engine:           s.Engine,
		Version:          s.Version,
		Pins:             copyStringMap(s.Pins),
		Memory:           s.Memory,
		CPU:              s.CPU,
		Port:             s.Port,
		Difficulty:       s.Difficulty,
		Gamemode:         s.Gamemode,
		MaxPlayers:       s.MaxPlayers,
		OnlineMode:       s.OnlineMode,
		Motd:             s.Motd,
		WhitelistEnabled: s.WhitelistEnabled,
		Whitelist:        copyStringList(s.Whitelist),
		Ops:              copyStringList(s.Ops),
		Seed:             s.Seed,
		Plugins:          copyStringList(s.Plugins),
		CreatedAt:        Stamp(),
	}
}

// Validate comprueba que el manifiesto es RECREABLE: motor conocido, versión
// exacta y todos los pines obligatorios del motor, ninguno "latest".
func (mf Manifest) Validate() error {
	e, err := engines.EngineOf(mf.Engine)
	if err != nil {
		return err
	}
	if v := strings.TrimSpace(mf.Version); v == "" || strings.EqualFold(v, "latest") {
		return errx.Usage("el manifiesto no fija una versión exacta de Minecraft",
			"exporta de nuevo la instancia; nunca se acepta 'latest'")
	}
	for _, dep := range engines.RequiredPins(e) {
		pin := strings.TrimSpace(mf.Pins[dep])
		if pin == "" || strings.EqualFold(pin, "latest") {
			return errx.Usage("el manifiesto no fija el pin obligatorio "+dep+" del motor "+e.Name,
				"exporta de nuevo la instancia desde una configuración válida")
		}
	}
	if host.ParseMemory(mf.Memory) == 0 {
		return errx.Usage("memoria inválida en el manifiesto: "+strconvQuote(mf.Memory), "usa un valor como 4G")
	}
	return nil
}

// ToSpec materializa el manifiesto en una definición de instancia con el slug
// indicado. No consulta la red: los pins ya venían fijados.
func (mf Manifest) ToSpec(slug string) Spec {
	s := Spec{
		Slug:             slug,
		Name:             strings.TrimSpace(mf.Name),
		Engine:           mf.Engine,
		Version:          mf.Version,
		Pins:             copyStringMap(mf.Pins),
		Memory:           mf.Memory,
		CPU:              mf.CPU,
		Port:             mf.Port,
		Difficulty:       mf.Difficulty,
		Gamemode:         mf.Gamemode,
		MaxPlayers:       mf.MaxPlayers,
		OnlineMode:       mf.OnlineMode,
		Motd:             mf.Motd,
		WhitelistEnabled: mf.WhitelistEnabled,
		Whitelist:        copyStringList(mf.Whitelist),
		Ops:              copyStringList(mf.Ops),
		Seed:             mf.Seed,
		Plugins:          copyStringList(mf.Plugins),
		CreatedAt:        Stamp(),
		SchemaVersion:    SchemaVersion,
	}
	if s.Name == "" {
		s.Name = slug
	}
	if strings.TrimSpace(s.Memory) == "" {
		s.Memory = "4G"
	}
	if s.MaxPlayers <= 0 {
		s.MaxPlayers = 20
	}
	if s.Port <= 0 {
		s.Port = 25565
	}
	return s
}

// ExportManifest describe una instancia registrada. Se niega a exportar una
// configuración inválida: un manifiesto que no se puede recrear no sirve.
func (m *Manager) ExportManifest(slug string) (Manifest, error) {
	spec, err := m.Load(slug)
	if err != nil {
		return Manifest{}, err
	}
	if err := spec.Validate(m.engines); err != nil {
		return Manifest{}, errx.Wrap(errx.CodeGeneral, err,
			"la instancia no es reproducible: su configuración es inválida",
			"revisa 'badomens instance inspect'")
	}
	return ManifestFromSpec(*spec), nil
}

// ImportManifest recrea la configuración de una instancia desde un manifiesto.
// Con target vacío se usa el slug del manifiesto. No sobrescribe nada salvo que
// overwrite sea true.
func (m *Manager) ImportManifest(mf Manifest, target string, overwrite bool) (*Spec, error) {
	if err := mf.Validate(); err != nil {
		return nil, err
	}
	slug := strings.TrimSpace(target)
	if slug == "" {
		slug = mf.Slug
	}
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}
	if _, statErr := os.Stat(m.SpecPath(slug)); statErr == nil && !overwrite {
		return nil, errx.Usage("la instancia "+slug+" ya existe",
			"usa --replace para sobrescribir su configuración o indica otro slug")
	}
	spec := mf.ToSpec(slug)
	if err := spec.Validate(m.engines); err != nil {
		return nil, errx.Wrap(errx.CodeGeneral, err,
			"el manifiesto no produce una instancia válida", "revisa el manifiesto")
	}
	if err := m.Save(&spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

// ReadManifest lee y valida un manifiesto de disco (JSON).
func ReadManifest(path string) (Manifest, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- la ruta la elige el operador
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Manifest{}, errx.NotFound("no existe el manifiesto "+path, "comprueba la ruta del archivo")
		}
		return Manifest{}, err
	}
	var mf Manifest
	if err := json.Unmarshal(raw, &mf); err != nil {
		return Manifest{}, errx.Wrap(errx.CodeUsage, err,
			"el manifiesto no es JSON válido", "usa un archivo exportado con 'badomens repro export'")
	}
	if mf.Schema != "" && mf.Schema != ManifestSchema {
		return Manifest{}, errx.Usage("esquema de manifiesto no soportado: "+mf.Schema,
			"se espera "+ManifestSchema)
	}
	mf.Schema = ManifestSchema
	return mf, nil
}

// WriteManifest escribe el manifiesto en disco con permisos de solo lectura para
// el usuario (0600). Es atómico: temporal + rename.
func WriteManifest(path string, mf Manifest) error {
	mf.Schema = ManifestSchema
	raw, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ManifestJSON serializa el manifiesto para imprimirlo (misma forma que el
// archivo, sin escribirlo).
func ManifestJSON(mf Manifest) ([]byte, error) {
	mf.Schema = ManifestSchema
	raw, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyStringList(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// strconvQuote evita importar strconv solo por una comilla.
func strconvQuote(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}
