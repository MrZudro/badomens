package svc

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/runtime"
	"github.com/MrZudro/badomens/internal/worlds"
)

// Worlds gestiona la biblioteca de mundos y el mundo activo de una instancia.
type Worlds interface {
	// Library lista los mundos guardados en la biblioteca compartida.
	Library(ctx context.Context) ([]string, error)
	// Regenerate borra el mundo activo (overworld, nether y end) de la instancia
	// y fija `seed` (vacío = aleatoria) en su definición. Si la instancia estaba
	// corriendo, la detiene antes y la vuelve a arrancar para regenerar.
	Regenerate(ctx context.Context, slug, seed string) error
	// ImportZip importa `archive` a la biblioteca como `name` y lo despliega como
	// mundo activo de la instancia. Si estaba corriendo, la detiene y reanuda.
	ImportZip(ctx context.Context, slug, archive, name string) error
}

// realWorlds implementa Worlds sobre el registro de instancias, el runtime y la
// biblioteca de mundos. Toda operación que escriba en el mundo activo detiene
// antes el servidor (si corría) y lo reanuda al terminar, para que Minecraft no
// sobrescriba los cambios al guardar.
type realWorlds struct {
	cfg *config.Config
	m   *instance.Manager
	rt  *runtime.Runtime
	wm  *worlds.Manager
}

var _ Worlds = (*realWorlds)(nil)

// instanceDir devuelve la carpeta de la instancia.
func (r *realWorlds) instanceDir(slug string) string {
	if r.m != nil {
		return r.m.Dir(slug)
	}
	if r.cfg != nil {
		return filepath.Join(r.cfg.DataDir, "instances", slug)
	}
	return slug
}

// activeWorldDirs son las carpetas del mundo activo: overworld, nether y end.
func (r *realWorlds) activeWorldDirs(slug string) []string {
	base := filepath.Join(r.instanceDir(slug), "server")
	return []string{
		filepath.Join(base, "world"),
		filepath.Join(base, "world_nether"),
		filepath.Join(base, "world_the_end"),
	}
}

// running informa de si la instancia está corriendo. Sin runtime (tests) se
// asume que no lo está. Un fallo al consultar el estado se propaga: es preferible
// abortar a escribir en el mundo de un servidor que podría estar en marcha.
func (r *realWorlds) running(ctx context.Context, slug string) (bool, error) {
	if r.rt == nil {
		return false, nil
	}
	state, err := r.rt.State(ctx, slug)
	if err != nil {
		return false, err
	}
	return state == "running", nil
}

// stopIfRunning detiene la instancia si estaba corriendo y dice si lo estaba.
func (r *realWorlds) stopIfRunning(ctx context.Context, slug string) (bool, error) {
	was, err := r.running(ctx, slug)
	if err != nil || !was {
		return was, err
	}
	return true, r.rt.Stop(ctx, slug)
}

// Regenerate borra el mundo activo y guarda la nueva semilla en el registro.
func (r *realWorlds) Regenerate(ctx context.Context, slug, seed string) error {
	spec, err := r.m.Load(slug)
	if err != nil {
		return err
	}
	was, err := r.stopIfRunning(ctx, slug)
	if err != nil {
		return err
	}
	for _, dir := range r.activeWorldDirs(slug) {
		// os.RemoveAll no falla si la carpeta no existe (nunca arrancó).
		if rerr := os.RemoveAll(dir); rerr != nil {
			return rerr
		}
	}
	spec.Seed = strings.TrimSpace(seed)
	if err := r.m.Save(spec); err != nil {
		return err
	}
	if !was {
		return nil
	}
	return r.rt.Start(ctx, spec)
}

// ImportZip importa un zip a la biblioteca y lo despliega como mundo activo.
func (r *realWorlds) ImportZip(ctx context.Context, slug, archive, name string) error {
	spec, err := r.m.Load(slug)
	if err != nil {
		return err
	}
	world := normalizeWorldName(name)
	if world == "" {
		world = worldNameFromArchive(archive)
	}
	if world == "" {
		return errx.Usage(
			"no se pudo determinar el nombre del mundo",
			"indica un nombre con letras, números, guiones o guiones bajos",
		)
	}
	was, err := r.stopIfRunning(ctx, slug)
	if err != nil {
		return err
	}
	if err := r.wm.ImportFromZip(archive, world); err != nil {
		return err
	}
	if err := r.wm.UseWorld(r.instanceDir(slug), world); err != nil {
		return err
	}
	if !was {
		return nil
	}
	return r.rt.Start(ctx, spec)
}

// Library lista la biblioteca compartida de mundos.
func (r *realWorlds) Library(_ context.Context) ([]string, error) {
	if r.wm == nil {
		return []string{}, nil
	}
	list, err := r.wm.List()
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []string{}
	}
	return list, nil
}

// worldNameReplacer translitera los acentos más comunes antes de filtrar.
var worldNameReplacer = strings.NewReplacer(
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
)

// normalizeWorldName convierte un nombre libre en un nombre de mundo válido:
// minúsculas, espacios a guiones y solo [a-z0-9-_]. Devuelve "" si no queda nada.
func normalizeWorldName(raw string) string {
	s := worldNameReplacer.Replace(strings.ToLower(strings.TrimSpace(raw)))
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '\t' || r == '.':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-_")
}

// worldNameFromArchive deriva el nombre del mundo del nombre base del archivo.
// Acepta tanto «/» como «\»: una ruta elegida en Windows lleva barras invertidas
// y el mismo texto debe normalizarse igual en cualquier sistema operativo.
func worldNameFromArchive(archive string) string {
	clean := strings.ReplaceAll(strings.TrimSpace(archive), `\`, "/")
	base := filepath.Base(clean)
	return normalizeWorldName(strings.TrimSuffix(base, filepath.Ext(base)))
}

// WorldCall registra una llamada a un método de Worlds (tests de la TUI).
type WorldCall struct {
	// Op es "library", "regenerate" o "import".
	Op      string
	Slug    string
	Seed    string
	Archive string
	Name    string
}

// FakeWorlds es el adaptador determinista de Worlds para los tests (sin disco ni
// Docker): registra cada llamada y mantiene la biblioteca en memoria. Se obtiene
// del servicio con una aserción de tipo: s.Worlds.(*svc.FakeWorlds).
type FakeWorlds struct {
	mu      sync.Mutex
	library []string
	known   map[string]bool
	calls   []WorldCall
	err     error
}

var _ Worlds = (*FakeWorlds)(nil)

// newFakeWorlds inicializa el fake desde el registro de instancias y las opciones.
func newFakeWorlds(specs []*instance.Spec, opts FakeOptions) *FakeWorlds {
	known := make(map[string]bool, len(specs))
	for _, s := range specs {
		known[s.Slug] = true
	}
	library := append([]string{}, opts.WorldLibrary...)
	sort.Strings(library)
	return &FakeWorlds{library: library, known: known, err: opts.WorldErr}
}

// Library devuelve la biblioteca en memoria.
func (f *FakeWorlds) Library(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, WorldCall{Op: "library"})
	if f.err != nil {
		return nil, f.err
	}
	return append([]string{}, f.library...), nil
}

// Regenerate registra la regeneración (slug + semilla).
func (f *FakeWorlds) Regenerate(_ context.Context, slug, seed string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, WorldCall{Op: "regenerate", Slug: slug, Seed: seed})
	if f.err != nil {
		return f.err
	}
	return f.unknown(slug)
}

// ImportZip registra la importación (archivo + nombre) y añade el mundo a la
// biblioteca en memoria.
func (f *FakeWorlds) ImportZip(_ context.Context, slug, archive, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, WorldCall{Op: "import", Slug: slug, Archive: archive, Name: name})
	if f.err != nil {
		return f.err
	}
	if err := f.unknown(slug); err != nil {
		return err
	}
	if name != "" {
		f.library = append(append([]string{}, f.library...), name)
		sort.Strings(f.library)
	}
	return nil
}

// unknown comprueba el slug contra el registro; si el registro está vacío no
// comprueba nada (para no estorbar a los tests que no declaran instancias).
func (f *FakeWorlds) unknown(slug string) error {
	if len(f.known) == 0 || f.known[slug] {
		return nil
	}
	return errx.NotFound("la instancia "+slug+" no existe", "elige una instancia del registro")
}

// Calls devuelve las llamadas registradas, en orden.
func (f *FakeWorlds) Calls() []WorldCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]WorldCall{}, f.calls...)
}

// LibraryNames devuelve la biblioteca en memoria.
func (f *FakeWorlds) LibraryNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.library...)
}

// Reset limpia las llamadas registradas (la biblioteca se conserva).
func (f *FakeWorlds) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}
