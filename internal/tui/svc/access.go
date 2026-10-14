package svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/runtime"
)

// AccessSource indica de dónde procede el estado de acceso mostrado.
type AccessSource string

const (
	AccessLive        AccessSource = "live"        // leído del servidor (datos en caliente)
	AccessSavedConfig AccessSource = "saved"       // solo configuración guardada (no verificable en vivo)
	AccessUnavailable AccessSource = "unavailable" // no se pudo consultar
)

// AccessState es el estado de acceso de una instancia.
type AccessState struct {
	WhitelistEnabled bool
	Whitelist        []string
	Operators        []string
	VerifiedAt       time.Time
	Source           AccessSource
}

// Access valida y gestiona whitelist y operadores con lectura verificable.
type Access interface {
	Read(ctx context.Context, slug string) (AccessState, error)
	AddWhitelist(ctx context.Context, slug, player string) (AccessState, error)
	RemoveWhitelist(ctx context.Context, slug, player string) (AccessState, error)
	SetWhitelistEnabled(ctx context.Context, slug string, enabled bool) (AccessState, error)
	AddOperator(ctx context.Context, slug, player string) (AccessState, error)
	RemoveOperator(ctx context.Context, slug, player string) (AccessState, error)
}

// ValidatePlayerName normaliza (trim) y valida un nombre de jugador; rechaza
// vacío y caracteres de control. Devuelve el nombre normalizado.
func ValidatePlayerName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errors.New("el nombre del jugador no puede estar vacío")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("el nombre del jugador %q contiene caracteres de control", raw)
		}
	}
	return name, nil
}

// realAccess implementa Access sobre el registro de instancias y el runtime.
// Lee el estado en vivo de los ficheros del servidor y muta enviando comandos
// por la consola con read-back verificable.
type realAccess struct {
	m  *instance.Manager
	rt *runtime.Runtime
}

var _ Access = (*realAccess)(nil)

// playerEntry es una entrada de whitelist.json / ops.json.
type playerEntry struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

// Read carga la instancia e intenta leer el estado en vivo. Si los ficheros del
// servidor no existen (nunca arrancó o está apagado) cae a la configuración
// guardada del spec. Un JSON corrupto degrada a AccessUnavailable con error.
func (r *realAccess) Read(_ context.Context, slug string) (AccessState, error) {
	spec, err := r.m.Load(slug)
	if err != nil {
		return AccessState{}, err
	}
	st, ok, rerr := r.readLive(spec)
	if rerr != nil {
		return AccessState{Source: AccessUnavailable}, rerr
	}
	if ok {
		return st, nil
	}
	return savedState(spec), nil
}

// readLive intenta leer whitelist.json, ops.json y server.properties del
// directorio de datos del servidor. Devuelve ok=false si no hay ninguno de los
// ficheros de jugadores (servidor apagado o nunca arrancado).
func (r *realAccess) readLive(spec *instance.Spec) (AccessState, bool, error) {
	dir := filepath.Join(r.rt.InstanceDir(spec), "server")

	wlNames, wlExists, err := readPlayerFile(filepath.Join(dir, "whitelist.json"))
	if err != nil {
		return AccessState{}, false, err
	}
	opNames, opsExists, err := readPlayerFile(filepath.Join(dir, "ops.json"))
	if err != nil {
		return AccessState{}, false, err
	}
	if !wlExists && !opsExists {
		return AccessState{}, false, nil
	}

	st := AccessState{
		WhitelistEnabled: spec.WhitelistEnabled, // respaldo si falta server.properties
		Whitelist:        normalizeList(wlNames),
		Operators:        normalizeList(opNames),
		VerifiedAt:       time.Now(),
		Source:           AccessLive,
	}
	if enabled, ok := readWhitelistEnabled(filepath.Join(dir, "server.properties")); ok {
		st.WhitelistEnabled = enabled
	}
	return st, true, nil
}

// savedState construye el estado a partir de la configuración guardada.
func savedState(spec *instance.Spec) AccessState {
	return AccessState{
		WhitelistEnabled: spec.WhitelistEnabled,
		Whitelist:        normalizeList(spec.Whitelist),
		Operators:        normalizeList(spec.Ops),
		Source:           AccessSavedConfig,
	}
}

// AddWhitelist añade un jugador a la whitelist y verifica con read-back.
func (r *realAccess) AddWhitelist(ctx context.Context, slug, player string) (AccessState, error) {
	name, err := ValidatePlayerName(player)
	if err != nil {
		return AccessState{}, err
	}
	return r.mutate(ctx, slug, "whitelist add "+name, func(s *instance.Spec) {
		s.Whitelist = addName(s.Whitelist, name)
	})
}

// RemoveWhitelist quita un jugador de la whitelist y verifica con read-back.
func (r *realAccess) RemoveWhitelist(ctx context.Context, slug, player string) (AccessState, error) {
	name, err := ValidatePlayerName(player)
	if err != nil {
		return AccessState{}, err
	}
	return r.mutate(ctx, slug, "whitelist remove "+name, func(s *instance.Spec) {
		s.Whitelist = removeName(s.Whitelist, name)
	})
}

// SetWhitelistEnabled enciende o apaga la whitelist del servidor.
func (r *realAccess) SetWhitelistEnabled(ctx context.Context, slug string, enabled bool) (AccessState, error) {
	command := "whitelist off"
	if enabled {
		command = "whitelist on"
	}
	return r.mutate(ctx, slug, command, func(s *instance.Spec) {
		s.WhitelistEnabled = enabled
	})
}

// AddOperator concede operador a un jugador y verifica con read-back.
func (r *realAccess) AddOperator(ctx context.Context, slug, player string) (AccessState, error) {
	name, err := ValidatePlayerName(player)
	if err != nil {
		return AccessState{}, err
	}
	return r.mutate(ctx, slug, "op "+name, func(s *instance.Spec) {
		s.Ops = addName(s.Ops, name)
	})
}

// RemoveOperator retira el operador a un jugador y verifica con read-back.
func (r *realAccess) RemoveOperator(ctx context.Context, slug, player string) (AccessState, error) {
	name, err := ValidatePlayerName(player)
	if err != nil {
		return AccessState{}, err
	}
	return r.mutate(ctx, slug, "deop "+name, func(s *instance.Spec) {
		s.Ops = removeName(s.Ops, name)
	})
}

// mutate envía el comando por consola y hace read-back. Si la lectura vuelve a
// ser en vivo, devuelve ese estado (éxito verificado). Si no es verificable
// (servidor apagado o ficheros aún no escritos), devuelve el estado guardado
// con el cambio aplicado en memoria —sin persistirlo— para que la UI muestre
// "enviado, pendiente de verificar". Si Console falla, devuelve el error.
func (r *realAccess) mutate(ctx context.Context, slug, command string, apply func(*instance.Spec)) (AccessState, error) {
	spec, err := r.m.Load(slug)
	if err != nil {
		return AccessState{}, err
	}
	if err := r.rt.Console(ctx, slug, command); err != nil {
		return AccessState{}, err
	}
	if st, ok, rerr := r.readLive(spec); rerr == nil && ok {
		return st, nil
	}
	// Read-back no verificable: aplica de forma idempotente sin persistir.
	apply(spec)
	return savedState(spec), nil
}

// readPlayerFile lee un fichero de jugadores tipo [{"uuid":…,"name":…}].
// Devuelve exists=false cuando el fichero no está; un JSON inválido es error.
func readPlayerFile(path string) ([]string, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var entries []playerEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, false, fmt.Errorf("estado de acceso corrupto en %s: %w", filepath.Base(path), err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if name := strings.TrimSpace(e.Name); name != "" {
			names = append(names, name)
		}
	}
	return names, true, nil
}

// readWhitelistEnabled busca white-list=true en server.properties.
func readWhitelistEnabled(path string) (bool, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "white-list" {
			continue
		}
		return strings.EqualFold(strings.TrimSpace(val), "true"), true
	}
	return false, false
}

// normalizeList recorta, deduplica (sin distinguir mayúsculas) y ordena una
// lista de nombres.
func normalizeList(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		key := strings.ToLower(s)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// addName añade un nombre de forma idempotente (comparación sin distinguir
// mayúsculas).
func addName(list []string, name string) []string {
	for _, n := range list {
		if strings.EqualFold(n, name) {
			return list
		}
	}
	return append(list, name)
}

// removeName quita un nombre de forma idempotente (comparación sin distinguir
// mayúsculas).
func removeName(list []string, name string) []string {
	out := make([]string, 0, len(list))
	for _, n := range list {
		if !strings.EqualFold(n, name) {
			out = append(out, n)
		}
	}
	return out
}

// fakeAccess es el adaptador determinista en memoria para los tests de la TUI.
type fakeAccess struct {
	mu         sync.Mutex
	states     map[string]AccessState
	savedOnly  bool
	readErr    error
	verifyFail bool
}

var _ Access = (*fakeAccess)(nil)

// newFakeAccess inicializa el fake desde las definiciones de instancia.
func newFakeAccess(specs []*instance.Spec, opts FakeOptions) *fakeAccess {
	states := make(map[string]AccessState, len(specs))
	for _, s := range specs {
		states[s.Slug] = AccessState{
			WhitelistEnabled: s.WhitelistEnabled,
			Whitelist:        normalizeList(s.Whitelist),
			Operators:        normalizeList(s.Ops),
		}
	}
	return &fakeAccess{
		states:     states,
		savedOnly:  opts.AccessSavedOnly,
		readErr:    opts.AccessReadErr,
		verifyFail: opts.AccessVerifyFail,
	}
}

// Read devuelve el estado en memoria. Con readErr fuerza unavailable; con
// savedOnly simula un servidor apagado (solo configuración guardada).
func (f *fakeAccess) Read(_ context.Context, slug string) (AccessState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return AccessState{Source: AccessUnavailable}, f.readErr
	}
	st, ok := f.states[slug]
	if !ok {
		return AccessState{Source: AccessUnavailable}, fmt.Errorf("la instancia %q no existe", slug)
	}
	if f.savedOnly {
		st.Source = AccessSavedConfig
		st.VerifiedAt = time.Time{}
		return st, nil
	}
	st.Source = AccessLive
	st.VerifiedAt = time.Now()
	return st, nil
}

// mutate aplica el cambio y devuelve el estado tras el read-back. Con
// verifyFail (o savedOnly) el cambio queda aplicado pero sin verificar en vivo.
func (f *fakeAccess) mutate(slug string, apply func(*AccessState)) (AccessState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.states[slug]
	if !ok {
		return AccessState{Source: AccessUnavailable}, fmt.Errorf("la instancia %q no existe", slug)
	}
	apply(&st)
	st.Whitelist = normalizeList(st.Whitelist)
	st.Operators = normalizeList(st.Operators)
	if f.verifyFail || f.savedOnly {
		st.Source = AccessSavedConfig
		st.VerifiedAt = time.Time{}
	} else {
		st.Source = AccessLive
		st.VerifiedAt = time.Now()
	}
	f.states[slug] = st
	return st, nil
}

// AddWhitelist añade un jugador validado a la whitelist.
func (f *fakeAccess) AddWhitelist(_ context.Context, slug, player string) (AccessState, error) {
	name, err := ValidatePlayerName(player)
	if err != nil {
		return AccessState{}, err
	}
	return f.mutate(slug, func(st *AccessState) { st.Whitelist = append(st.Whitelist, name) })
}

// RemoveWhitelist quita un jugador de la whitelist.
func (f *fakeAccess) RemoveWhitelist(_ context.Context, slug, player string) (AccessState, error) {
	name, err := ValidatePlayerName(player)
	if err != nil {
		return AccessState{}, err
	}
	return f.mutate(slug, func(st *AccessState) { st.Whitelist = removeName(st.Whitelist, name) })
}

// SetWhitelistEnabled enciende o apaga la whitelist.
func (f *fakeAccess) SetWhitelistEnabled(_ context.Context, slug string, enabled bool) (AccessState, error) {
	return f.mutate(slug, func(st *AccessState) { st.WhitelistEnabled = enabled })
}

// AddOperator concede operador a un jugador validado.
func (f *fakeAccess) AddOperator(_ context.Context, slug, player string) (AccessState, error) {
	name, err := ValidatePlayerName(player)
	if err != nil {
		return AccessState{}, err
	}
	return f.mutate(slug, func(st *AccessState) { st.Operators = append(st.Operators, name) })
}

// RemoveOperator retira el operador a un jugador.
func (f *fakeAccess) RemoveOperator(_ context.Context, slug, player string) (AccessState, error) {
	name, err := ValidatePlayerName(player)
	if err != nil {
		return AccessState{}, err
	}
	return f.mutate(slug, func(st *AccessState) { st.Operators = removeName(st.Operators, name) })
}
