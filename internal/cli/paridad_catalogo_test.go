package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/output"
	"github.com/MrZudro/badomens/internal/tui/svc"
)

// ===========================================================================
// Paridad de la fachada CLI / TUI / MCP sobre el servicio de catálogo (Fase 5)
//
// Integración 6.2 punto 8: para la MISMA entrada (motor + versión), las tres
// rutas deben devolver el MISMO motor, la MISMA versión y los MISMOS pins, porque
// las tres pasan por el mismo caso de uso (CatalogService, T-01).
// ===========================================================================

// catalogoProvider es un proveedor de catálogo de prueba, sin red.
type catalogoProvider struct {
	engine engines.Engine
	list   []engines.Version
	pin    string
}

func (p *catalogoProvider) Engine() engines.Engine { return p.engine }

func (p *catalogoProvider) ListVersions(context.Context) ([]engines.Version, error) {
	return p.list, nil
}

func (p *catalogoProvider) Resolve(_ context.Context, version string) (engines.Resolved, error) {
	v := strings.TrimSpace(version)
	if v == "" {
		v = p.list[len(p.list)-1].Version
	}
	ok := false
	for _, x := range p.list {
		if x.Version == v {
			ok = true
			break
		}
	}
	if !ok {
		return engines.Resolved{}, &engines.TransportError{
			Code: engines.CodeNotFound, Provider: p.engine.Name, Operation: "resolve",
			Err: fmt.Errorf("la versión %q no existe para este motor", v),
		}
	}
	return engines.Resolved{
		Version: v,
		Pins:    map[string]string{engines.Paper.DepVar: p.pin},
		Source:  p.engine.Name,
	}, nil
}

// catalogoVersions es el catálogo de prueba de las tres rutas.
func catalogoVersions() []engines.Version {
	return []engines.Version{
		{Version: "1.20.1", Stable: true, Channel: engines.ChannelRelease, Compatibility: engines.CompatibilityCompatible},
		{Version: "1.21.1", Stable: true, Channel: engines.ChannelRelease, Compatibility: engines.CompatibilityCompatible},
		{Version: "26.3", Stable: true, Channel: engines.ChannelRelease, Compatibility: engines.CompatibilityCompatible},
	}
}

// paridadEnv prepara un Env aislado cuyo servicio de catálogo es el controlado.
func paridadEnv(t *testing.T) (*Env, *bytes.Buffer, *engines.CatalogService, string) {
	t.Helper()
	dataDir := t.TempDir()
	var out, errBuf bytes.Buffer
	provider := &catalogoProvider{engine: engines.Paper, list: catalogoVersions(), pin: "123"}
	service := engines.NewCatalogService(engines.NewCache(dataDir, engines.NewRegistryFrom(provider)))
	env := &Env{
		stdout:  &out,
		stderr:  &errBuf,
		cfg:     &config.Config{DataDir: dataDir},
		printer: output.New(&out, &errBuf, output.JSON),
		catalogFactory: func() (*engines.CatalogService, error) {
			return service, nil
		},
	}
	return env, &out, service, dataDir
}

// paridadSeedInstance registra la instancia que usará `engine use`.
func paridadSeedInstance(t *testing.T, dataDir string) {
	t.Helper()
	m := instance.New(dataDir, engineNames())
	spec := &instance.Spec{
		Slug: "demo", Name: "demo", Engine: "vanilla", Version: "1.20.1",
		Memory: "4G", Port: 25565, MaxPlayers: 20, CreatedAt: instance.Stamp(),
	}
	if err := m.Save(spec); err != nil {
		t.Fatalf("no se pudo preparar la instancia: %v", err)
	}
}

// asStringMap convierte el mapa de pins del documento JSON a map[string]string.
func asStringMap(t *testing.T, raw any) map[string]string {
	t.Helper()
	if already, ok := raw.(map[string]string); ok {
		return already
	}
	m, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("se esperaba un objeto de pins, tengo %T (%v)", raw, raw)
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("el pin %q no es una cadena: %T", k, v)
		}
		out[k] = s
	}
	return out
}

// TestParidadResolucionCLITUIMCP es el caso central de la Fase 5: la resolución
// de la misma entrada coincide en las tres fachadas.
func TestParidadResolucionCLITUIMCP(t *testing.T) {
	env, out, service, dataDir := paridadEnv(t)
	paridadSeedInstance(t, dataDir)

	// --- Ruta CLI: `engine use --no-apply` (escribe la configuración) ---
	doc, _ := runEngine(t, env, out, "engine", "use", "demo", "paper", "--version", "1.21.1", "--no-apply")
	if doc["ok"] != true {
		t.Fatalf("engine use falló: %v", doc)
	}
	cliData, _ := doc["data"].(map[string]any)
	if cliData == nil {
		t.Fatalf("engine use debe publicar data: %v", doc)
	}

	// --- Ruta TUI: la fachada svc sobre el MISMO servicio ---
	resolved, err := svc.NewCatalog(service).Resolve(context.Background(), "paper", "1.21.1")
	if err != nil {
		t.Fatalf("svc.Catalog.Resolve: %v", err)
	}

	// --- Ruta MCP: herramienta del registro único ---
	registry, err := env.Registry()
	if err != nil {
		t.Fatalf("registro de herramientas: %v", err)
	}
	args, _ := json.Marshal(map[string]any{"engine": "paper", "version": "1.21.1"})
	mcpRes, err := registry.Invoke(context.Background(), "badomens_engine_resolve", args)
	if err != nil {
		t.Fatalf("MCP badomens_engine_resolve: %v", err)
	}
	if mcpRes.Data == nil {
		t.Fatal("la herramienta MCP debe publicar datos estructurados")
	}

	// Motor, versión y pins IDÉNTICOS en las tres rutas.
	if cliData["engine"] != "paper" || resolved.Source != "paper" || mcpRes.Data["engine"] != "paper" {
		t.Fatalf("motor: cli=%v tui=%v mcp=%v", cliData["engine"], resolved.Source, mcpRes.Data["engine"])
	}
	if cliData["version"] != "1.21.1" || resolved.Version != "1.21.1" || mcpRes.Data["version"] != "1.21.1" {
		t.Fatalf("versión: cli=%v tui=%v mcp=%v", cliData["version"], resolved.Version, mcpRes.Data["version"])
	}
	cliPins := asStringMap(t, cliData["pins"])
	if !reflect.DeepEqual(cliPins, resolved.Pins) {
		t.Fatalf("pins CLI %v ≠ TUI %v", cliPins, resolved.Pins)
	}
	if mcpPins := asStringMap(t, mcpRes.Data["pins"]); !reflect.DeepEqual(mcpPins, resolved.Pins) {
		t.Fatalf("pins MCP %v ≠ TUI %v", mcpPins, resolved.Pins)
	}
	// Y los campos normalizados aditivos también coinciden.
	// La ruta CLI pasa por el documento JSON (los números llegan como float64),
	// así que se comparan por su representación estable.
	for _, key := range []string{"compatibility", "java_major", "java_status", "source"} {
		if fmt.Sprint(cliData[key]) != fmt.Sprint(mcpRes.Data[key]) {
			t.Fatalf("%s: CLI=%v MCP=%v", key, cliData[key], mcpRes.Data[key])
		}
	}
	if cliData["catalog"] == nil || mcpRes.Data["catalog"] == nil {
		t.Fatalf("las tres rutas deben exponer el estado del catálogo: %v / %v", cliData["catalog"], mcpRes.Data["catalog"])
	}

	// La selección se guarda POR VALOR (T-04): la instancia queda con la versión
	// y los pins resueltos, no con un índice ni con "latest".
	spec, err := instance.New(dataDir, engineNames()).Load("demo")
	if err != nil {
		t.Fatalf("cargar la instancia: %v", err)
	}
	if spec.Version != resolved.Version || !reflect.DeepEqual(spec.Pins, resolved.Pins) {
		t.Fatalf("la instancia quedó con %s/%v; quiero %s/%v", spec.Version, spec.Pins, resolved.Version, resolved.Pins)
	}
}

// TestParidadErrorNormalizadoCLITUIMCP: las tres rutas reciben el MISMO error
// normalizado (código estable) ante una versión inexistente.
func TestParidadErrorNormalizadoCLITUIMCP(t *testing.T) {
	env, out, service, dataDir := paridadEnv(t)
	paridadSeedInstance(t, dataDir)

	// CLI: el documento declara error_not_found.
	doc, _ := runEngine(t, env, out, "engine", "use", "demo", "paper", "--version", "9.9.9", "--no-apply")
	if doc["ok"] != false || doc["exit_code"] != float64(2) {
		t.Fatalf("se esperaba un fallo con exit 2: %v", doc)
	}
	cliErr, _ := doc["error"].(map[string]any)
	if cliErr["code"] != errx.CodeNotFound {
		t.Fatalf("código CLI = %v; quiero %s", cliErr["code"], errx.CodeNotFound)
	}

	// TUI: error tipado de errx.
	_, err := svc.NewCatalog(service).Resolve(context.Background(), "paper", "9.9.9")
	if !errx.Is(err, errx.CodeNotFound) {
		t.Fatalf("error TUI = %v; quiero %s", err, errx.CodeNotFound)
	}

	// MCP: mismo código estable.
	registry, rerr := env.Registry()
	if rerr != nil {
		t.Fatalf("registro: %v", rerr)
	}
	args, _ := json.Marshal(map[string]any{"engine": "paper", "version": "9.9.9"})
	if _, err := registry.Invoke(context.Background(), "badomens_engine_resolve", args); !errx.Is(err, errx.CodeNotFound) {
		t.Fatalf("error MCP = %v; quiero %s", err, errx.CodeNotFound)
	}
}

// TestParidadListadoCLIMCP: el listado de versiones del CLI y el de MCP son el
// mismo, con el mismo orden y la misma recomendada.
func TestParidadListadoCLIMCP(t *testing.T) {
	env, out, _, _ := paridadEnv(t)

	doc, _ := runEngine(t, env, out, "engine", "list", "paper")
	cliList, _ := doc["data"].([]any)

	registry, err := env.Registry()
	if err != nil {
		t.Fatalf("registro: %v", err)
	}
	args, _ := json.Marshal(map[string]any{"engine": "paper"})
	mcpRes, err := registry.Invoke(context.Background(), "badomens_engine_versions", args)
	if err != nil {
		t.Fatalf("MCP badomens_engine_versions: %v", err)
	}
	mcpVersions, _ := mcpRes.Data["versions"].([]map[string]any)
	if len(cliList) != len(mcpVersions) {
		t.Fatalf("CLI %d versiones ≠ MCP %d", len(cliList), len(mcpVersions))
	}
	for i := range cliList {
		if cliList[i] != mcpVersions[i]["id"] {
			t.Fatalf("posición %d: CLI=%v MCP=%v", i, cliList[i], mcpVersions[i]["id"])
		}
	}
	if mcpRes.Data["catalog"] == nil || mcpRes.Data["stale"] != false {
		t.Fatalf("MCP debe exponer estado y stale: %v", mcpRes.Data)
	}
}

// TestContratoJSONCatalogoAditivo (AC-09): los documentos de `engine versions`,
// `engine list`, `catalog status` y `catalog refresh` conservan las claves
// estables y solo AÑADEN campos.
func TestContratoJSONCatalogoAditivo(t *testing.T) {
	env, out, _, _ := paridadEnv(t)

	cases := []struct {
		name string
		run  func() (map[string]any, error)
	}{
		{"engine versions", func() (map[string]any, error) {
			return runEngine(t, env, out, "engine", "versions", "paper")
		}},
		{"engine list", func() (map[string]any, error) {
			return runEngine(t, env, out, "engine", "list", "paper")
		}},
		{"catalog status", func() (map[string]any, error) {
			return runCatalog(t, env, out, "catalog", "status", "paper")
		}},
		{"catalog refresh", func() (map[string]any, error) {
			return runCatalog(t, env, out, "catalog", "refresh", "paper")
		}},
	}
	for _, tc := range cases {
		doc, _ := tc.run()
		for _, key := range []string{"ok", "exit_code", "action", "message", "timestamp"} {
			if _, ok := doc[key]; !ok {
				t.Fatalf("%s: falta la clave estable %q en %v", tc.name, key, doc)
			}
		}
	}

	// `engine versions` mantiene sus claves y añade los campos normalizados.
	doc, _ := runEngine(t, env, out, "engine", "versions", "paper")
	data, _ := doc["data"].(map[string]any)
	for _, key := range []string{"engine", "stale", "catalog", "versions"} {
		if _, ok := data[key]; !ok {
			t.Fatalf("engine versions: falta %q en %v", key, data)
		}
	}
	versions, _ := data["versions"].([]any)
	first, _ := versions[0].(map[string]any)
	for _, key := range []string{
		"id", "channel", "recommended", "java_major", "build", "released_at",
		"compatible", "incompatible_why", "compatibility", "java_status",
		"loader_version", "detail", "source", "fetched_at", "stale", "warning",
	} {
		if _, ok := first[key]; !ok {
			t.Fatalf("falta el campo %q en la versión: %v", key, first)
		}
	}
	catalog, _ := data["catalog"].(map[string]any)
	for _, key := range []string{"engine", "state", "source", "versions", "ttl", "validated", "usable", "age", "age_seconds"} {
		if _, ok := catalog[key]; !ok {
			t.Fatalf("falta %q en el estado del catálogo: %v", key, catalog)
		}
	}

	// `catalog status` mantiene `catalogs` con su estado.
	doc, _ = runCatalog(t, env, out, "catalog", "status", "paper")
	data, _ = doc["data"].(map[string]any)
	catalogs, _ := data["catalogs"].([]any)
	if len(catalogs) != 1 {
		t.Fatalf("catalogs = %v", data["catalogs"])
	}
}

// TestMCPStatusYCompatEstructurados: las herramientas MCP nuevas publican
// estados estructurados (nunca mensajes visuales).
func TestMCPStatusYCompatEstructurados(t *testing.T) {
	env, _, _, _ := paridadEnv(t)
	registry, err := env.Registry()
	if err != nil {
		t.Fatalf("registro: %v", err)
	}

	args, _ := json.Marshal(map[string]any{"engine": "paper"})
	res, err := registry.Invoke(context.Background(), "badomens_catalog_status", args)
	if err != nil {
		t.Fatalf("badomens_catalog_status: %v", err)
	}
	catalogs, _ := res.Data["catalogs"].([]map[string]any)
	if len(catalogs) != 1 || catalogs[0]["state"] == nil || catalogs[0]["usable"] == nil {
		t.Fatalf("estado estructurado incompleto: %v", res.Data)
	}
	if _, ok := catalogs[0]["versions"].(int); !ok {
		t.Fatalf("versions debe ser numérico: %v", catalogs[0]["versions"])
	}

	// Compatibilidad: Java verificado → compatible; sin Java → unknown.
	compatArgs, _ := json.Marshal(map[string]any{
		"engine": "paper", "version": "1.21.1", "java_major": 21, "build": "123", "fill": true,
	})
	compat, err := registry.Invoke(context.Background(), "badomens_engine_compat", compatArgs)
	if err != nil {
		t.Fatalf("badomens_engine_compat: %v", err)
	}
	if compat.Data["compatibility"] != string(engines.CompatibilityCompatible) {
		t.Fatalf("compatibilidad = %v", compat.Data["compatibility"])
	}

	unknownArgs, _ := json.Marshal(map[string]any{"engine": "paper", "version": "1.21.1"})
	unknown, err := registry.Invoke(context.Background(), "badomens_engine_compat", unknownArgs)
	if err != nil {
		t.Fatalf("badomens_engine_compat (sin java): %v", err)
	}
	if unknown.Data["compatibility"] != string(engines.CompatibilityUnknown) {
		t.Fatalf("un dato desconocido NUNCA es compatible: %v", unknown.Data)
	}

	// Con plugins declarados: conflicto comprobado → incompatible.
	pluginArgs, _ := json.Marshal(map[string]any{
		"engine": "paper", "version": "1.21.1", "java_major": 21, "build": "123", "fill": true,
		"plugins": []map[string]any{{"name": "FabricAPI", "loaders": []string{"fabric"}}},
	})
	conflict, err := registry.Invoke(context.Background(), "badomens_engine_compat", pluginArgs)
	if err != nil {
		t.Fatalf("badomens_engine_compat (plugins): %v", err)
	}
	if conflict.Data["compatibility"] != string(engines.CompatibilityIncompatible) {
		t.Fatalf("conflicto de plugin = %v", conflict.Data)
	}
	if strings.TrimSpace(fmt.Sprint(conflict.Data["reason"])) == "" {
		t.Fatalf("quiero el motivo estructurado: %v", conflict.Data)
	}
}

// TestRegistryToolsCatalogoRegistradas comprueba que las herramientas nuevas
// están en el registro único (y son de solo lectura).
func TestRegistryToolsCatalogoRegistradas(t *testing.T) {
	env, _, _, _ := paridadEnv(t)
	registry, err := env.Registry()
	if err != nil {
		t.Fatalf("registro: %v", err)
	}
	for _, name := range []string{
		"badomens_engine_versions", "badomens_engine_resolve",
		"badomens_catalog_status", "badomens_engine_compat",
	} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("falta la herramienta %q", name)
		}
		if !tool.ReadOnly {
			t.Fatalf("%q debe ser de solo lectura", name)
		}
		if _, err := json.Marshal(tool.JSONSchema()); err != nil {
			t.Fatalf("%q: esquema inválido: %v", name, err)
		}
	}
}

// TestComandoEngineCompatCLI: el veredicto trivalente se traduce a códigos de
// salida estables (0 compatible, 1 desconocido, 2 incompatible).
func TestComandoEngineCompatCLI(t *testing.T) {
	env, out, _, _ := paridadEnv(t)

	doc, _ := runEngine(t, env, out, "engine", "compat", "paper", "--version", "1.21.1", "--java", "21", "--build", "123")
	if doc["exit_code"] != float64(0) || doc["ok"] != true {
		t.Fatalf("compatible debe salir 0: %v", doc)
	}
	if data, _ := doc["data"].(map[string]any); data["compatibility"] != "compatible" {
		t.Fatalf("compatibilidad = %v", doc["data"])
	}

	doc, _ = runEngine(t, env, out, "engine", "compat", "paper", "--version", "1.21.1")
	if doc["exit_code"] != float64(1) {
		t.Fatalf("desconocido debe avisar (exit 1): %v", doc)
	}

	doc, _ = runEngine(t, env, out, "engine", "compat", "paper", "--version", "1.21.1",
		"--java", "21", "--fill", "--plugin", "FabricAPI@fabric@1.21.1")
	if doc["exit_code"] != float64(2) {
		t.Fatalf("incompatible debe fallar (exit 2): %v", doc)
	}

	// Con --fill se completan los datos que falten desde el catálogo.
	doc, _ = runEngine(t, env, out, "engine", "compat", "paper", "--version", "1.21.1", "--fill")
	if doc["exit_code"] != float64(0) {
		t.Fatalf("--fill debe completar y salir 0: %v", doc)
	}
}
