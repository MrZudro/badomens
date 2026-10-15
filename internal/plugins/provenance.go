package plugins

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/instance"
)

// ProvenanceFile es el nombre del registro de procedencia de una instancia. Vive
// junto al instance.yaml, NUNCA dentro de plugins/ ni mods/: el servidor recorre
// esas carpetas y no debe encontrarse archivos que no sean artefactos.
const ProvenanceFile = "plugins.json"

// ProvenanceSchemaVersion es la versión del esquema de este registro. El registro
// es ADITIVO: los campos nuevos se añaden y los antiguos se siguen leyendo.
const ProvenanceSchemaVersion = 1

// Origen desconocido: una instalación de la que no se guardó procedencia (por
// ejemplo, anterior a la existencia de este registro). Coincide con el
// vocabulario de internal/plugins/marketplace.
const originUnknown = "desconocido"

// Origin describe de dónde salió un plugin/mod instalado. Todos los campos son
// opcionales: un registro antiguo solo tiene el nombre, y eso se respeta.
type Origin struct {
	// Source identifica el marketplace o la vía de origen ("modrinth",
	// "hangar", "url", "local", "desconocido").
	Source string `json:"source"`
	// ProjectID, ProjectSlug y ProjectName identifican el proyecto de origen.
	ProjectID   string `json:"project_id,omitempty"`
	ProjectSlug string `json:"project_slug,omitempty"`
	ProjectName string `json:"project_name,omitempty"`
	// VersionID identifica la versión publicada de origen.
	VersionID string `json:"version_id,omitempty"`
	// URL es la dirección del artefacto, sin credenciales.
	URL string `json:"url,omitempty"`
	// Channel es el canal declarado por el proveedor (release, beta...).
	Channel string `json:"channel,omitempty"`
	// Kind es el tipo de proyecto de origen (plugin, mod...).
	Kind string `json:"kind,omitempty"`
}

// Known informa si la procedencia se conoce.
func (o Origin) Known() bool {
	return strings.TrimSpace(o.Source) != "" && !strings.EqualFold(o.Source, originUnknown)
}

// Record es una entrada del registro de procedencia: qué archivo está instalado,
// de dónde salió, qué versión es y con qué checksum se verificó.
type Record struct {
	// Name es el nombre del archivo instalado (con sufijo si se renombró).
	Name string `json:"name"`
	// Version es la versión INSTALADA (tal y como la numera el proveedor).
	Version string `json:"version,omitempty"`
	// TargetVersion es la versión OBJETIVO detectada por la comprobación de
	// actualizaciones. Se rellena solo cuando hay una versión nueva publicada y
	// NO se ha instalado: comprobar actualizaciones no instala nada.
	TargetVersion string `json:"target_version,omitempty"`
	// SHA256 y SHA512 son los checksums conocidos del artefacto instalado.
	SHA256 string `json:"sha256,omitempty"`
	SHA512 string `json:"sha512,omitempty"`
	// SizeBytes es el tamaño instalado.
	SizeBytes int64 `json:"size_bytes,omitempty"`
	// Loader y GameVersion describen el entorno para el que se resolvió.
	Loader      string `json:"loader,omitempty"`
	GameVersion string `json:"game_version,omitempty"`
	// InstalledAt es el instante de la instalación (RFC3339 UTC).
	InstalledAt string `json:"installed_at,omitempty"`
	// Origin describe la procedencia.
	Origin Origin `json:"origin"`
}

// Register es el registro de procedencia completo de una instancia.
type Register struct {
	// SchemaVersion versiona el formato; los registros antiguos se leen igual.
	SchemaVersion int `json:"schema_version"`
	// Records son las entradas, una por archivo instalado.
	Records []Record `json:"records"`
}

// NewRegister crea un registro vacío.
func NewRegister() *Register {
	return &Register{SchemaVersion: ProvenanceSchemaVersion}
}

// Get devuelve la entrada de un archivo.
func (r *Register) Get(name string) (Record, bool) {
	if r == nil {
		return Record{}, false
	}
	for _, rec := range r.Records {
		if strings.EqualFold(rec.Name, name) {
			return rec, true
		}
	}
	return Record{}, false
}

// Upsert añade o reemplaza la entrada de un archivo: una instalación deja
// EXACTAMENTE una entrada por nombre, nunca duplicados.
func (r *Register) Upsert(rec Record) {
	if r == nil || strings.TrimSpace(rec.Name) == "" {
		return
	}
	r.SchemaVersion = ProvenanceSchemaVersion
	for i := range r.Records {
		if strings.EqualFold(r.Records[i].Name, rec.Name) {
			r.Records[i] = rec
			return
		}
	}
	r.Records = append(r.Records, rec)
}

// Remove borra la entrada de un archivo. Devuelve si existía.
func (r *Register) Remove(name string) bool {
	if r == nil {
		return false
	}
	for i := range r.Records {
		if strings.EqualFold(r.Records[i].Name, name) {
			r.Records = append(r.Records[:i], r.Records[i+1:]...)
			return true
		}
	}
	return false
}

// Len devuelve el número de entradas.
func (r *Register) Len() int {
	if r == nil {
		return 0
	}
	return len(r.Records)
}

// Names devuelve los nombres registrados, ordenados.
func (r *Register) Names() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.Records))
	for _, rec := range r.Records {
		out = append(out, rec.Name)
	}
	sort.Strings(out)
	return out
}

// Merge cruza el registro de procedencia con la lista de plugins instalados que
// mantiene la instancia (instance.Spec.Plugins, en instance.yaml).
//
// Es la pieza de MIGRACIÓN ADITIVA: un nombre instalado que no tiene entrada de
// procedencia —una instalación antigua, hecha antes de que existiera este
// registro— se devuelve con origen "desconocido". Nunca se inventa procedencia,
// ni versión, ni checksum: se dice lo que se sabe.
func (r *Register) Merge(names []string) []Record {
	want := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[strings.ToLower(n)] {
			continue
		}
		seen[strings.ToLower(n)] = true
		want = append(want, n)
	}
	sort.Strings(want)

	out := make([]Record, 0, len(want))
	for _, name := range want {
		if rec, ok := r.Get(name); ok {
			out = append(out, rec)
			continue
		}
		out = append(out, Record{Name: name, Origin: Origin{Source: originUnknown}})
	}
	return out
}

// Installed devuelve lo que hay instalado según las DOS fuentes que se
// complementan: el registro de procedencia (lo instalado con badomens) y la
// lista de la instancia (instance.Spec.Plugins, que el comando mantiene al día).
//
// Es la vista que se usa para EVALUAR dependencias y actualizaciones: una
// instalación no debe perderse por estar registrada solo en una de las dos
// fuentes. Los nombres que solo aparecen en la lista de la instancia se
// devuelven con origen desconocido, sin inventar procedencia.
func (r *Register) Installed(names []string) []Record {
	if r == nil {
		return NewRegister().Installed(names)
	}
	byName := map[string]Record{}
	for _, rec := range r.Records {
		name := strings.TrimSpace(rec.Name)
		if name == "" {
			continue
		}
		byName[strings.ToLower(name)] = rec
	}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if _, ok := byName[strings.ToLower(n)]; ok {
			continue
		}
		byName[strings.ToLower(n)] = Record{Name: n, Origin: Origin{Source: originUnknown}}
	}
	out := make([]Record, 0, len(byName))
	for _, rec := range byName {
		out = append(out, rec)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Orphans devuelve las entradas del registro cuyo archivo ya no está en la lista

// de instalados: son restos de un borrado hecho a mano.
func (r *Register) Orphans(names []string) []Record {
	if r == nil {
		return nil
	}
	live := map[string]bool{}
	for _, n := range names {
		live[strings.ToLower(strings.TrimSpace(n))] = true
	}
	var out []Record
	for _, rec := range r.Records {
		if !live[strings.ToLower(rec.Name)] {
			out = append(out, rec)
		}
	}
	return out
}

// Provenance es el registro persistente de procedencia de una instancia.
type Provenance struct {
	// DataDir es el mismo directorio de datos que usa el Installer.
	DataDir string
}

// NewProvenance crea el registro de procedencia.
func NewProvenance(dataDir string) *Provenance { return &Provenance{DataDir: dataDir} }

// Path devuelve la ruta del archivo de procedencia de una instancia.
func (p *Provenance) Path(spec *instance.Spec) string {
	slug := ""
	if spec != nil {
		slug = spec.Slug
	}
	return filepath.Join(p.DataDir, "instances", slug, ProvenanceFile)
}

// Load lee el registro. Si no existe devuelve uno vacío (no es un error: la
// instancia puede no tener ninguna instalación con procedencia). Un archivo
// ilegible SÍ es un error: es preferible fallar que perder la procedencia en
// silencio.
func (p *Provenance) Load(spec *instance.Spec) (*Register, error) {
	path := p.Path(spec)
	raw, err := os.ReadFile(path) // #nosec G304 -- ruta derivada del data dir y el slug de la instancia
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewRegister(), nil
		}
		return nil, errx.Wrap(errx.CodeGeneral, err, "no se pudo leer el registro de plugins de la instancia",
			"revisa permisos en "+path)
	}
	reg := NewRegister()
	if err := json.Unmarshal(raw, reg); err != nil {
		return nil, errx.Wrap(errx.CodeGeneral, err, "el registro de plugins de la instancia está corrupto",
			"revisa o borra "+path+"; los archivos instalados no se tocan")
	}
	if reg.SchemaVersion < ProvenanceSchemaVersion {
		// Migración aditiva: no hay nada que reescribir, solo se marca la versión
		// al guardar. Los registros antiguos (solo nombre) siguen leyéndose.
		reg.SchemaVersion = ProvenanceSchemaVersion
	}
	reg.sanitize()
	return reg, nil
}

// Save escribe el registro de forma atómica (temporal en el mismo directorio +
// rename).
func (p *Provenance) Save(spec *instance.Spec, reg *Register) error {
	if reg == nil {
		return errx.New(errx.CodeGeneral, "no hay registro de procedencia que guardar", "")
	}
	reg.SchemaVersion = ProvenanceSchemaVersion
	reg.sanitize()
	path := p.Path(spec)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errx.Wrap(errx.CodeGeneral, err, "no se pudo preparar la carpeta del registro de plugins",
			"revisa permisos en "+dir)
	}
	raw, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return errx.Wrap(errx.CodeGeneral, err, "no se pudo serializar el registro de procedencia", "")
	}
	raw = append(raw, '\n')

	tmp, err := os.CreateTemp(dir, ProvenanceFile+".tmp-*")
	if err != nil {
		return errx.Wrap(errx.CodeGeneral, err, "no se pudo crear el archivo temporal del registro",
			"revisa permisos y espacio en disco")
	}
	tmpName := tmp.Name()
	writeErr := func() error {
		if _, werr := tmp.Write(raw); werr != nil {
			return werr
		}
		if serr := tmp.Sync(); serr != nil {
			return serr
		}
		return tmp.Close()
	}()
	if writeErr != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return errx.Wrap(errx.CodeGeneral, writeErr, "no se pudo escribir el registro de procedencia", "")
	}
	if rerr := os.Rename(tmpName, path); rerr != nil {
		// Windows no reemplaza con os.Rename.
		if runtime.GOOS == "windows" {
			if derr := os.Remove(path); derr == nil || errors.Is(derr, os.ErrNotExist) {
				if rerr = os.Rename(tmpName, path); rerr == nil {
					return nil
				}
			}
		}
		_ = os.Remove(tmpName)
		return errx.Wrap(errx.CodeGeneral, rerr, "no se pudo guardar el registro de procedencia",
			"revisa permisos en "+path)
	}
	return nil
}

// sanitize normaliza el registro al leerlo o al guardarlo: descarta entradas sin
// nombre utilizable y fija la versión del esquema. Un nombre con separadores de
// ruta se descarta: el registro nunca debe poder apuntar fuera de la carpeta de
// plugins.
func (r *Register) sanitize() {
	if r == nil {
		return
	}
	out := make([]Record, 0, len(r.Records))
	seen := map[string]bool{}
	for _, rec := range r.Records {
		name := strings.TrimSpace(rec.Name)
		if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
			continue
		}
		if seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		rec.Name = name
		rec.SHA256 = strings.ToLower(strings.TrimSpace(rec.SHA256))
		rec.SHA512 = strings.ToLower(strings.TrimSpace(rec.SHA512))
		if rec.Origin.Source == "" {
			rec.Origin.Source = originUnknown
		}
		out = append(out, rec)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	r.Records = out
}

// Timestamp formatea un instante en el formato del registro.
func Timestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
