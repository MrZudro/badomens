package cli

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/plugins"
	"github.com/MrZudro/badomens/internal/plugins/marketplace"
	"github.com/MrZudro/badomens/internal/plugins/marketplace/modrinth"
	"github.com/spf13/cobra"
)

// --- Esquema de alambre de Modrinth (el parser real es el del adaptador) ------

type mktFile struct {
	Hashes   map[string]string `json:"hashes"`
	URL      string            `json:"url"`
	Filename string            `json:"filename"`
	Primary  bool              `json:"primary"`
	Size     int64             `json:"size"`
}

type mktDep struct {
	ProjectID      *string `json:"project_id"`
	VersionID      *string `json:"version_id"`
	FileName       *string `json:"file_name"`
	DependencyType string  `json:"dependency_type"`
}

type mktVersion struct {
	Loaders       *[]string `json:"loaders"`
	GameVersions  *[]string `json:"game_versions"`
	ID            string    `json:"id"`
	ProjectID     string    `json:"project_id"`
	Name          string    `json:"name"`
	VersionNumber string    `json:"version_number"`
	DatePublished string    `json:"date_published"`
	VersionType   string    `json:"version_type"`
	Status        string    `json:"status"`
	Files         []mktFile `json:"files"`
	Dependencies  []mktDep  `json:"dependencies"`
}

type mktProject struct {
	ID           string   `json:"id"`
	Slug         string   `json:"slug"`
	ProjectType  string   `json:"project_type"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Status       string   `json:"status"`
	Loaders      []string `json:"loaders"`
	GameVersions []string `json:"game_versions"`
}

type mktHit struct {
	ProjectID   string   `json:"project_id"`
	ProjectType string   `json:"project_type"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Categories  []string `json:"categories"`
	Versions    []string `json:"versions"`
	Downloads   int64    `json:"downloads"`
	License     string   `json:"license"`
}

// mktFixture es un marketplace de Modrinth simulado: mismo esquema, misma
// paginación y mismo filtrado en el servidor que la API real. NO toca la red.
type mktFixture struct {
	api      *httptest.Server
	artifact *httptest.Server
	jar      []byte
	sha256   string
	sha512   string
	hits     []mktHit

	projects map[string]mktProject
	versions map[string][]mktVersion

	// projectStatus fuerza el estado HTTP de /project/{ref}; 0 ⇒ normal.
	projectStatus int
	// searchStatus fuerza el estado HTTP de /search; 0 ⇒ normal.
	searchStatus int
}

// newMktFixture monta los dos servidores (API y artefacto) y un JAR válido.
func newMktFixture(t *testing.T) *mktFixture {
	t.Helper()
	jar := jarDemo(t)
	sum256 := sha256.Sum256(jar)
	sum512 := sha512.Sum512(jar)
	f := &mktFixture{
		jar:      jar,
		sha256:   hex.EncodeToString(sum256[:]),
		sha512:   hex.EncodeToString(sum512[:]),
		projects: map[string]mktProject{},
		versions: map[string][]mktVersion{},
	}

	f.artifact = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/java-archive")
		_, _ = w.Write(f.jar)
	}))
	t.Cleanup(f.artifact.Close)

	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if f.searchStatus != 0 {
			http.Error(w, "boom", f.searchStatus)
			return
		}
		query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("query")))
		var hits []mktHit
		for _, h := range f.hits {
			if query == "" || strings.Contains(strings.ToLower(h.Slug), query) ||
				strings.Contains(strings.ToLower(h.Title), query) {
				hits = append(hits, h)
			}
		}
		if hits == nil {
			hits = []mktHit{}
		}
		offset := atoiOr(r.URL.Query().Get("offset"), 0)
		limit := atoiOr(r.URL.Query().Get("limit"), 20)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"hits": hits, "total_hits": len(hits), "offset": offset, "limit": limit,
		})
	})
	mux.HandleFunc("/project/{ref}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if f.projectStatus != 0 {
			http.Error(w, "boom", f.projectStatus)
			return
		}
		p, ok := f.projects[r.PathValue("ref")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(p)
	})
	mux.HandleFunc("/project/{ref}/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ref := r.PathValue("ref")
		if _, ok := f.projects[ref]; !ok {
			http.NotFound(w, r)
			return
		}
		loaders := jsonList(r.URL.Query().Get("loaders"))
		game := jsonList(r.URL.Query().Get("game_versions"))
		out := make([]mktVersion, 0)
		for _, v := range f.versions[ref] {
			if len(loaders) > 0 && !declares(*v.Loaders, loaders) {
				continue
			}
			if len(game) > 0 && !declares(*v.GameVersions, game) {
				continue
			}
			out = append(out, v)
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	f.api = httptest.NewServer(mux)
	t.Cleanup(f.api.Close)
	return f
}

// addProject registra un proyecto en el catálogo simulado (por slug y por id).
func (f *mktFixture) addProject(p mktProject, versions ...mktVersion) {
	f.projects[p.Slug] = p
	if p.ID != "" {
		f.projects[p.ID] = p
		f.versions[p.ID] = versions
	}
	f.versions[p.Slug] = versions
}

// version construye una versión publicada con el artefacto servido por el
// servidor de artefactos del fixture.
func (f *mktFixture) version(number, date, channel string, loaders, game []string, hashes map[string]string, deps ...mktDep) mktVersion {
	name := "ViaVersion-" + number + ".jar"
	l, g := loaders, game
	return mktVersion{
		Loaders: &l, GameVersions: &g,
		ID: "id-" + number, ProjectID: "P1OZGk5p", Name: "ViaVersion " + number,
		VersionNumber: number, DatePublished: date, VersionType: channel, Status: "listed",
		Files: []mktFile{{
			Hashes: hashes, URL: f.artifact.URL + "/" + name,
			Filename: name, Primary: true, Size: int64(len(f.jar)),
		}},
		Dependencies: deps,
	}
}

// rel dep es un atajo para una dependencia obligatoria declarada por id.
func relDep(projectID, name string) mktDep {
	return mktDep{ProjectID: &projectID, FileName: &name, DependencyType: "required"}
}

// apiCatalog devuelve el catálogo de marketplaces apuntando al fixture.
func (f *mktFixture) catalog(t *testing.T) *marketplace.Catalog {
	t.Helper()
	client := engines.DefaultClient()
	client.MaxAttempts = 1
	m := modrinth.New(client)
	m.BaseURL = f.api.URL
	cat := marketplace.NewCatalog()
	cat.Register(m)
	return cat
}

// withFixtureCatalog inyecta el catálogo del fixture en el CLI y lo restaura al
// terminar la prueba.
func withFixtureCatalog(t *testing.T, f *mktFixture) {
	t.Helper()
	cat := f.catalog(t)
	prev := newPluginMarketCatalog
	newPluginMarketCatalog = func() (*marketplace.Catalog, error) { return cat, nil }
	t.Cleanup(func() { newPluginMarketCatalog = prev })
}

// runMarketCmd ejecuta un subcomando de marketplace suelto.
func runMarketCmd(t *testing.T, cmd *cobra.Command, args ...string) error {
	t.Helper()
	root := &cobra.Command{Use: "root", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(cmd)
	root.SetArgs(append([]string{cmd.Name()}, args...))
	return root.Execute()
}

// marketDoc valida el contrato JSON estable y devuelve el documento.
func marketDoc(t *testing.T, out *bytes.Buffer, action string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("la salida no es un documento JSON válido: %v\n%s", err, out.String())
	}
	for _, key := range []string{"ok", "exit_code", "action", "message", "timestamp"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("falta la clave estable %q: %v", key, doc)
		}
	}
	if doc["action"] != action {
		t.Errorf("action = %v, quiero %q", doc["action"], action)
	}
	return doc
}

// marketData devuelve el mapa data del documento.
func marketData(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	data, _ := doc["data"].(map[string]any)
	if data == nil {
		t.Fatalf("falta data: %v", doc)
	}
	return data
}

// marketErr devuelve el error del documento y sus detalles.
func marketErr(t *testing.T, doc map[string]any) (map[string]any, map[string]any) {
	t.Helper()
	errInfo, _ := doc["error"].(map[string]any)
	if errInfo == nil {
		t.Fatalf("falta error: %v", doc)
	}
	details, _ := errInfo["details"].(map[string]any)
	return errInfo, details
}

// pluginDir es la carpeta destino de plugins de la instancia demo.
func pluginDir(dataDir string) string {
	return filepath.Join(dataDir, "instances", "demo", "plugins")
}

// provenancePath es el registro de procedencia de la instancia demo.
func provenancePath(dataDir string) string {
	return filepath.Join(dataDir, "instances", "demo", plugins.ProvenanceFile)
}

// assertSinArchivos comprueba que la carpeta destino no existe o está vacía.
func assertSinArchivos(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("no debería haber nada instalado: %v", entries)
	}
}

// assertSinProcedencia comprueba que no se creó registro de procedencia.
func assertSinProcedencia(t *testing.T, dataDir string) {
	t.Helper()
	if _, err := os.Stat(provenancePath(dataDir)); err == nil {
		t.Errorf("no debería existir %s", provenancePath(dataDir))
	}
}

// atoiOr convierte un entero o devuelve el valor por defecto.
func atoiOr(raw string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return def
	}
	return n
}

// jsonList decodifica un array JSON de cadenas codificado en la query.
func jsonList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// declares informa si la lista declarada contiene alguno de los valores.
func declares(list, want []string) bool {
	for _, w := range want {
		for _, l := range list {
			if strings.EqualFold(strings.TrimSpace(w), strings.TrimSpace(l)) {
				return true
			}
		}
	}
	return false
}

// --- `plugin sources` --------------------------------------------------------

// TestPluginSourcesListaLosRegistrados: la lista de fuentes sale del catálogo
// real (modrinth + hangar) y NO consulta la red: el estado es el de registro.
func TestPluginSourcesListaLosRegistrados(t *testing.T) {
	env, out, _ := pluginTestEnv(t)
	if err := runMarketCmd(t, newPluginSourcesCmd(env)); err != nil {
		t.Fatalf("plugin sources: %v", err)
	}
	doc := marketDoc(t, out, "plugin sources")
	if doc["ok"] != true || doc["exit_code"] != float64(0) {
		t.Errorf("documento inesperado: %v", doc)
	}
	data := marketData(t, doc)
	if data["count"] != float64(2) {
		t.Errorf("count = %v, quiero 2", data["count"])
	}
	sources, _ := data["sources"].([]any)
	if len(sources) != 2 {
		t.Fatalf("sources = %v", data["sources"])
	}
	seen := map[string]bool{}
	for _, raw := range sources {
		m, _ := raw.(map[string]any)
		id, _ := m["id"].(string)
		seen[id] = true
		if m["status"] != "registrada" {
			t.Errorf("%s: status = %v", id, m["status"])
		}
		if m["live_checked"] != false || m["verified"] != false {
			t.Errorf("%s: no debe afirmar disponibilidad en vivo: %v", id, m)
		}
		if kinds, _ := m["kinds"].([]any); len(kinds) == 0 {
			t.Errorf("%s: sin tipos declarados: %v", id, m)
		}
	}
	if !seen["modrinth"] || !seen["hangar"] {
		t.Errorf("faltan fuentes: %v", seen)
	}
}

// --- `plugin search` ---------------------------------------------------------

// TestPluginSearchListaYsinResultados: una búsqueda traduce el resultado, NUNCA
// lo presenta como compatible verificado, y sin resultados lo dice claramente.
func TestPluginSearchListaYsinResultados(t *testing.T) {
	env, out, _ := pluginTestEnv(t)
	f := newMktFixture(t)
	f.hits = []mktHit{
		{
			ProjectID: "P1OZGk5p", ProjectType: "plugin", Slug: "viaversion", Title: "ViaVersion",
			Author: "ViaVersion", Categories: []string{"paper"}, Versions: []string{"1.21.4"},
			Downloads: 100, License: "mit",
		},
		{ProjectID: "P2", ProjectType: "mod", Slug: "viafabric", Title: "ViaFabric"},
	}
	f.addProject(mktProject{
		ID: "P1OZGk5p", Slug: "viaversion", ProjectType: "plugin", Title: "ViaVersion",
		Status: "approved", Loaders: []string{"paper"}, GameVersions: []string{"1.21.4"},
	})
	withFixtureCatalog(t, f)

	if err := runMarketCmd(t, newPluginSearchCmd(env), "via"); err != nil {
		t.Fatalf("plugin search: %v", err)
	}
	doc := marketDoc(t, out, "plugin search")
	if doc["ok"] != true || doc["exit_code"] != float64(0) {
		t.Errorf("documento inesperado: %v", doc)
	}
	data := marketData(t, doc)
	if data["count"] != float64(2) {
		t.Fatalf("count = %v (%v)", data["count"], data["results"])
	}
	results, _ := data["results"].([]any)
	first, _ := results[0].(map[string]any)
	// La búsqueda de Modrinth NO publica loaders: aunque el servidor haya
	// filtrado, el veredicto no puede ser "compatible".
	if first["compatibility"] != "desconocido" {
		t.Errorf("compatibility = %v; una búsqueda no demuestra compatibilidad", first["compatibility"])
	}
	if first["compatibility_verified"] != false {
		t.Errorf("compatibility_verified = %v", first["compatibility_verified"])
	}
	if first["slug"] != "viaversion" || first["kind"] != "plugin" {
		t.Errorf("resultado inesperado: %v", first)
	}
	// La búsqueda de Modrinth NO publica el estado de publicación: no se inventa.
	if first["publication"] != "desconocido" {
		t.Errorf("publication = %v", first["publication"])
	}
	env2, _ := data["environment"].(map[string]any)
	if env2 == nil || env2["loader"] != "paper" || env2["game_version"] != "1.21.4" {
		t.Errorf("environment = %v", data["environment"])
	}
	if note, _ := data["note"].(string); !strings.Contains(note, "no demuestra compatibilidad") {
		t.Errorf("falta el aviso de la búsqueda: %v", data["note"])
	}

	// Sin resultados: mensaje claro y exit 0.
	out.Reset()
	if err := runMarketCmd(t, newPluginSearchCmd(env), "zabroso"); err != nil {
		t.Fatalf("plugin search sin resultados: %v", err)
	}
	doc = marketDoc(t, out, "plugin search")
	if doc["ok"] != true || doc["exit_code"] != float64(0) {
		t.Errorf("documento inesperado: %v", doc)
	}
	data = marketData(t, doc)
	if data["count"] != float64(0) {
		t.Errorf("count = %v", data["count"])
	}
	if msg, _ := doc["message"].(string); !strings.Contains(msg, "sin resultados") {
		t.Errorf("message = %v", doc["message"])
	}
}

// TestPluginSearchFuenteDesconocida: un --source inexistente es un error de uso.
func TestPluginSearchFuenteDesconocida(t *testing.T) {
	env, out, _ := pluginTestEnv(t)
	if err := runMarketCmd(t, newPluginSearchCmd(env), "via", "--source", "curseforge"); err == nil {
		t.Fatal("se esperaba error por marketplace desconocido")
	}
	doc := marketDoc(t, out, "plugin search")
	if doc["exit_code"] != float64(2) {
		t.Errorf("exit_code = %v", doc["exit_code"])
	}
	_, details := marketErr(t, doc)
	if details["reason"] != "marketplace_no_registrado" {
		t.Errorf("details.reason = %v", details["reason"])
	}
}

// --- `plugin info` -----------------------------------------------------------

// TestPluginInfoDeclaradoYNoEncontrado: el detalle informa de la compatibilidad
// DECLARADA (nunca verificada) y un proyecto inexistente da not_found.
func TestPluginInfoDeclaradoYNoEncontrado(t *testing.T) {
	env, out, _ := pluginTestEnv(t)
	f := newMktFixture(t)
	f.addProject(mktProject{
		ID: "P1OZGk5p", Slug: "viaversion", ProjectType: "plugin", Title: "ViaVersion",
		Description: "Compatibilidad entre versiones", Status: "approved",
		Loaders: []string{"paper"}, GameVersions: []string{"1.21.4"},
	})
	withFixtureCatalog(t, f)

	if err := runMarketCmd(t, newPluginInfoCmd(env), "viaversion"); err != nil {
		t.Fatalf("plugin info: %v", err)
	}
	doc := marketDoc(t, out, "plugin info")
	data := marketData(t, doc)
	if data["source"] != "modrinth" {
		t.Errorf("source = %v", data["source"])
	}
	if data["compatibility"] != "compatible" {
		t.Errorf("compatibility = %v (%v)", data["compatibility"], data["compatibility_reason"])
	}
	if data["compatibility_verified"] != false {
		t.Errorf("compatibility_verified = %v; un detalle no demuestra compatibilidad", data["compatibility_verified"])
	}
	if data["publication"] != "publicado (approved)" || data["published"] != true {
		t.Errorf("publicación = %v/%v", data["publication"], data["published"])
	}
	proj, _ := data["project"].(map[string]any)
	if proj == nil || proj["slug"] != "viaversion" || proj["kind"] != "plugin" {
		t.Errorf("project = %v", data["project"])
	}
	if note, _ := data["note"].(string); !strings.Contains(note, "DECLARADA") {
		t.Errorf("note = %v", data["note"])
	}

	out.Reset()
	if err := runMarketCmd(t, newPluginInfoCmd(env), "no-existe"); err == nil {
		t.Fatal("se esperaba error para un proyecto inexistente")
	}
	doc = marketDoc(t, out, "plugin info")
	if doc["exit_code"] != float64(2) {
		t.Errorf("exit_code = %v", doc["exit_code"])
	}
	errInfo, details := marketErr(t, doc)
	if errInfo["code"] != "error_not_found" {
		t.Errorf("error.code = %v", errInfo["code"])
	}
	if details["reason"] != "no_encontrado" {
		t.Errorf("details.reason = %v", details["reason"])
	}
}

// --- `plugin versions` -------------------------------------------------------

// TestPluginVersionsFiltraPorEntorno: sin instancia se listan todas (con
// compatibilidad desconocida); con -i se piden solo las del entorno y
// --compatible descarta lo que el proveedor no declara para él.
func TestPluginVersionsFiltraPorEntorno(t *testing.T) {
	env, out, _ := pluginTestEnv(t)
	f := newMktFixture(t)
	f.addProject(mktProject{
		ID: "P1OZGk5p", Slug: "viaversion", ProjectType: "plugin", Title: "ViaVersion",
		Status: "approved", Loaders: []string{"paper", "fabric"}, GameVersions: []string{"1.21.4", "1.20.1"},
	},
		f.version("5.5.0", "2025-06-01T00:00:00Z", "release", []string{"paper"}, []string{"1.21.4"},
			map[string]string{"sha512": f.sha512, "sha1": strings.Repeat("a", 40)}),
		f.version("5.4.0", "2024-01-01T00:00:00Z", "release", []string{"paper"}, []string{"1.20.1"},
			map[string]string{"sha512": f.sha512}),
		f.version("6.0.0-beta1", "2025-07-01T00:00:00Z", "beta", []string{"fabric"}, []string{"1.21.4"},
			map[string]string{"sha512": f.sha512}),
	)
	withFixtureCatalog(t, f)

	// Con la instancia (paper + 1.21.4) el proveedor filtra y solo queda una.
	if err := runMarketCmd(t, newPluginVersionsCmd(env), "viaversion"); err != nil {
		t.Fatalf("plugin versions: %v", err)
	}
	doc := marketDoc(t, out, "plugin versions")
	data := marketData(t, doc)
	if data["count"] != float64(1) || data["total"] != float64(1) {
		t.Fatalf("count/total = %v/%v (%v)", data["count"], data["total"], data["versions"])
	}
	versions, _ := data["versions"].([]any)
	v0, _ := versions[0].(map[string]any)
	if v0["number"] != "5.5.0" || v0["channel"] != "release" {
		t.Errorf("versión inesperada: %v", v0)
	}
	if v0["compatibility"] != "compatible" {
		t.Errorf("compatibility = %v (%v)", v0["compatibility"], v0["compatibility_reason"])
	}
	if v0["compatibility_verified"] != false {
		t.Errorf("compatibility_verified = %v", v0["compatibility_verified"])
	}
	file, _ := v0["file"].(map[string]any)
	if file == nil || file["name"] != "ViaVersion-5.5.0.jar" || file["verifiable"] != true {
		t.Errorf("file = %v", v0["file"])
	}
	if h, _ := file["hash"].(map[string]any); h == nil || h["algorithm"] != "sha512" {
		t.Errorf("hash = %v", file["hash"])
	}

	// --compatible con instancia: misma versión.
	out.Reset()
	if err := runMarketCmd(t, newPluginVersionsCmd(env), "viaversion", "--compatible"); err != nil {
		t.Fatalf("plugin versions --compatible: %v", err)
	}
	doc = marketDoc(t, out, "plugin versions")
	data = marketData(t, doc)
	if data["count"] != float64(1) || data["compatible_only"] != true {
		t.Errorf("count/compatible_only = %v/%v", data["count"], data["compatible_only"])
	}

	// Sin instancia: todas las versiones, compatibilidad desconocida.
	env.instanceFlag = ""
	out.Reset()
	if err := runMarketCmd(t, newPluginVersionsCmd(env), "viaversion"); err != nil {
		t.Fatalf("plugin versions sin instancia: %v", err)
	}
	doc = marketDoc(t, out, "plugin versions")
	data = marketData(t, doc)
	if data["count"] != float64(3) || data["environment"] != nil {
		t.Fatalf("count/environment = %v/%v", data["count"], data["environment"])
	}
	versions, _ = data["versions"].([]any)
	for _, raw := range versions {
		v, _ := raw.(map[string]any)
		if v["compatibility"] != "desconocido" {
			t.Errorf("sin entorno no se puede afirmar compatibilidad: %v", v)
		}
	}

	// --compatible sin instancia: no hay entorno con el que comparar.
	out.Reset()
	if err := runMarketCmd(t, newPluginVersionsCmd(env), "viaversion", "--compatible"); err == nil {
		t.Fatal("se esperaba error de uso")
	}
	doc = marketDoc(t, out, "plugin versions")
	if doc["exit_code"] != float64(2) {
		t.Errorf("exit_code = %v", doc["exit_code"])
	}
	_, details := marketErr(t, doc)
	if details["reason"] != "entorno_incompleto" {
		t.Errorf("details.reason = %v", details["reason"])
	}
}

// TestPluginMarketRegistradoBajoPlugin: los subcomandos nuevos están colgados de
// `plugin` (regresión de registro en el árbol de cobra).
func TestPluginMarketRegistradoBajoPlugin(t *testing.T) {
	env, _, _ := pluginTestEnv(t)
	cmd := newPluginCmd(env)
	want := []string{"add", "list", "remove", "sources", "search", "info", "versions", "install", "updates"}
	got := map[string]bool{}
	for _, sub := range cmd.Commands() {
		got[sub.Name()] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("falta el subcomando plugin %s", name)
		}
	}
}

// --- `plugin install` --------------------------------------------------------

// mktDemoProject es el proyecto base de las pruebas de instalación.
func mktDemoProject() mktProject {
	return mktProject{
		ID: "P1OZGk5p", Slug: "viaversion", ProjectType: "plugin", Title: "ViaVersion",
		Status: "approved", Loaders: []string{"paper"}, GameVersions: []string{"1.21.4"},
	}
}

// seedInstalled deja un archivo ya instalado con su entrada de procedencia y su
// nombre en la lista de la instancia, como lo haría una instalación anterior.
func seedInstalled(t *testing.T, env *Env, dataDir, name string, rec plugins.Record) {
	t.Helper()
	m, err := env.manager()
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	spec, err := m.Load("demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	spec.Plugins = appendUnique(spec.Plugins, name)
	if err := m.Save(spec); err != nil {
		t.Fatalf("Save: %v", err)
	}
	prov := plugins.NewProvenance(dataDir)
	reg, err := prov.Load(spec)
	if err != nil {
		t.Fatalf("Load registro: %v", err)
	}
	reg.Upsert(rec)
	if err := prov.Save(spec, reg); err != nil {
		t.Fatalf("Save registro: %v", err)
	}
}

// addPluginName añade un nombre a la lista de la instancia SIN dejar entrada de
// procedencia: simula un archivo instalado a mano.
func addPluginName(t *testing.T, env *Env, name string) {
	t.Helper()
	m, err := env.manager()
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	spec, err := m.Load("demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	spec.Plugins = appendUnique(spec.Plugins, name)
	if err := m.Save(spec); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

// readRegister lee el registro de procedencia de la instancia demo.
func readRegister(t *testing.T, dataDir string, spec *instance.Spec) *plugins.Register {
	t.Helper()
	reg, err := plugins.NewProvenance(dataDir).Load(spec)
	if err != nil {
		t.Fatalf("Load registro: %v", err)
	}
	return reg
}

// assertFileUnchanged comprueba que un archivo no se ha tocado.
func assertFileUnchanged(t *testing.T, path string, before []byte, modTime string) {
	t.Helper()
	now, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(before, now) {
		t.Error("el archivo instalado cambió de contenido")
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if fi.ModTime().String() != modTime {
		t.Errorf("el archivo instalado cambió de fecha: %v ≠ %v", fi.ModTime(), modTime)
	}
}

// TestPluginInstallResolucionIncompleta: AC-06. Si la resolución no es completa,
// el comando falla con error tipado, deja resolution_complete:false y NO escribe
// nada: ni archivo, ni procedencia, ni lista de la instancia.
func TestPluginInstallResolucionIncompleta(t *testing.T) {
	cases := []struct {
		name   string
		arg    string
		reason string
		code   string
		// build añade al fixture las versiones publicadas.
		build func(f *mktFixture)
	}{
		{
			name: "sin version compatible", arg: "viaversion",
			reason: "sin_version_compatible", code: "error_usage",
			build: func(f *mktFixture) {
				f.addProject(mktDemoProject(), f.version("6.0.0", "2025-01-01T00:00:00Z", "release",
					[]string{"fabric"}, []string{"1.21.4"}, map[string]string{"sha512": f.sha512}))
			},
		},
		{
			name: "version inexistente", arg: "viaversion@9.9.9",
			reason: "version_inexistente", code: "error_not_found",
			build: func(f *mktFixture) {
				f.addProject(mktDemoProject(), f.version("5.5.0", "2025-01-01T00:00:00Z", "release",
					[]string{"paper"}, []string{"1.21.4"}, map[string]string{"sha512": f.sha512}))
			},
		},
		{
			name: "checksum exigible ausente", arg: "viaversion",
			reason: "checksum_ausente", code: "error_usage",
			build: func(f *mktFixture) {
				f.addProject(mktDemoProject(), f.version("5.5.0", "2025-01-01T00:00:00Z", "release",
					[]string{"paper"}, []string{"1.21.4"}, map[string]string{"sha1": strings.Repeat("a", 40)}))
			},
		},
		{
			name: "dependencia obligatoria ausente", arg: "viaversion",
			reason: "dependencia_no_satisfecha", code: "error_usage",
			build: func(f *mktFixture) {
				f.addProject(mktDemoProject(), f.version("5.5.0", "2025-01-01T00:00:00Z", "release",
					[]string{"paper"}, []string{"1.21.4"}, map[string]string{"sha512": f.sha512},
					relDep("P2Otro", "OtroPlugin")))
			},
		},
		{
			name: "proyecto no publicado", arg: "viaversion",
			reason: "proyecto_no_publicado", code: "error_usage",
			build: func(f *mktFixture) {
				p := mktDemoProject()
				p.Status = "draft"
				f.addProject(p, f.version("5.5.0", "2025-01-01T00:00:00Z", "release",
					[]string{"paper"}, []string{"1.21.4"}, map[string]string{"sha512": f.sha512}))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, out, dataDir := pluginTestEnv(t)
			f := newMktFixture(t)
			tc.build(f)
			withFixtureCatalog(t, f)

			err := runMarketCmd(t, newPluginInstallCmd(env), tc.arg, "--allow-http", "--allow-loopback")
			if err == nil {
				t.Fatal("se esperaba error de resolución incompleta")
			}
			doc := marketDoc(t, out, "plugin install")
			if doc["ok"] != false || doc["exit_code"] != float64(2) {
				t.Errorf("documento inesperado: %v", doc)
			}
			data := marketData(t, doc)
			if data["resolution_complete"] != false {
				t.Errorf("resolution_complete = %v; AC-06 exige false", data["resolution_complete"])
			}
			if data["stage"] != "resolucion" {
				t.Errorf("stage = %v", data["stage"])
			}
			problems, _ := data["problems"].([]any)
			if len(problems) == 0 {
				t.Fatalf("faltan los motivos de la resolución: %v", data)
			}
			p0, _ := problems[0].(map[string]any)
			if p0["reason"] != tc.reason {
				t.Errorf("problems[0].reason = %v, quiero %q", p0["reason"], tc.reason)
			}
			if p0["fatal"] != true {
				t.Errorf("problems[0].fatal = %v", p0["fatal"])
			}

			errInfo, details := marketErr(t, doc)
			if errInfo["code"] != tc.code {
				t.Errorf("error.code = %v, quiero %q", errInfo["code"], tc.code)
			}
			if details["reason"] != tc.reason {
				t.Errorf("error.details.reason = %v, quiero %q", details["reason"], tc.reason)
			}

			// Nada escrito: ni archivo, ni procedencia, ni lista.
			assertSinArchivos(t, pluginDir(dataDir))
			assertSinProcedencia(t, dataDir)
			spec, lerr := env.specFor("demo")
			if lerr != nil {
				t.Fatalf("Load: %v", lerr)
			}
			if len(spec.Plugins) != 0 {
				t.Errorf("la lista de la instancia no debe cambiar: %v", spec.Plugins)
			}
		})
	}
}

// TestPluginInstallCompletoRegistraUnaVez: instalación correcta (checksum
// publicado verificado): archivo en destino, procedencia y lista de la instancia
// actualizadas UNA sola vez, y reinstalar no duplica nada.
func TestPluginInstallCompletoRegistraUnaVez(t *testing.T) {
	env, out, dataDir := pluginTestEnv(t)
	f := newMktFixture(t)
	f.addProject(mktDemoProject(), f.version("5.5.0", "2025-06-01T00:00:00Z", "release",
		[]string{"paper"}, []string{"1.21.4"},
		map[string]string{"sha512": f.sha512, "sha1": strings.Repeat("a", 40)}))
	withFixtureCatalog(t, f)

	const name = "ViaVersion-5.5.0.jar"
	if err := runMarketCmd(t, newPluginInstallCmd(env), "viaversion", "--allow-http", "--allow-loopback"); err != nil {
		t.Fatalf("plugin install: %v", err)
	}
	doc := marketDoc(t, out, "plugin install")
	if doc["ok"] != true || doc["exit_code"] != float64(0) {
		t.Fatalf("documento inesperado: %v", doc)
	}
	data := marketData(t, doc)
	if data["source"] != "modrinth" || data["version"] != "5.5.0" {
		t.Errorf("source/version = %v/%v", data["source"], data["version"])
	}
	if data["resolution_complete"] != true || data["compatibility"] != "compatible" {
		t.Errorf("resolution_complete/compatibility = %v/%v", data["resolution_complete"], data["compatibility"])
	}
	if data["publication"] != "publicado (listed)" {
		t.Errorf("publication = %v", data["publication"])
	}
	hash, _ := data["hash"].(map[string]any)
	if hash == nil || hash["algorithm"] != "sha512" || hash["value"] != f.sha512 {
		t.Errorf("hash = %v", data["hash"])
	}
	installed, _ := data["installed"].(map[string]any)
	if installed == nil || installed["name"] != name || installed["checksum_verified"] != true {
		t.Errorf("installed = %v", data["installed"])
	}
	if installed["sha256"] != f.sha256 {
		t.Errorf("sha256 calculado = %v, quiero %v", installed["sha256"], f.sha256)
	}
	prov, _ := data["provenance"].(map[string]any)
	if prov == nil || prov["version"] != "5.5.0" || prov["sha512"] != f.sha512 {
		t.Errorf("provenance = %v", data["provenance"])
	}
	origin, _ := prov["origin"].(map[string]any)
	if origin["source"] != "modrinth" || origin["project_slug"] != "viaversion" || origin["project_id"] != "P1OZGk5p" {
		t.Errorf("provenance.origin = %v", origin)
	}
	if warn, _ := data["security_warning"].(string); !strings.Contains(warn, "terceros") {
		t.Errorf("falta el aviso de código de terceros: %v", data["security_warning"])
	}
	if _, hasWarnings := data["warnings"]; hasWarnings {
		t.Errorf("una instalación verificada no debería avisar: %v", data["warnings"])
	}

	// Destino: el archivo está y no quedan temporales.
	if _, err := os.Stat(filepath.Join(pluginDir(dataDir), name)); err != nil {
		t.Errorf("el archivo no está en el destino: %v", err)
	}
	assertSinTemporales(t, pluginDir(dataDir))

	spec, err := env.specFor("demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(spec.Plugins) != 1 || spec.Plugins[0] != name {
		t.Errorf("lista de la instancia = %v", spec.Plugins)
	}
	reg := readRegister(t, dataDir, spec)
	if reg.Len() != 1 || reg.Records[0].Name != name || reg.Records[0].Version != "5.5.0" {
		t.Fatalf("procedencia = %+v", reg.Records)
	}

	// Reinstalar el mismo archivo: falla y NO duplica el registro.
	out.Reset()
	if err := runMarketCmd(t, newPluginInstallCmd(env), "viaversion@5.5.0", "--allow-http", "--allow-loopback"); err == nil {
		t.Fatal("se esperaba error al reinstalar el mismo archivo")
	}
	doc = marketDoc(t, out, "plugin install")
	if doc["exit_code"] != float64(2) {
		t.Errorf("exit_code = %v", doc["exit_code"])
	}
	_, details := marketErr(t, doc)
	if details["reason"] != "destino_existente" {
		t.Errorf("details.reason = %v", details["reason"])
	}
	data = marketData(t, doc)
	if data["resolution_complete"] != true || data["stage"] != "instalacion" {
		t.Errorf("el fallo es de instalación, no de resolución: %v/%v", data["resolution_complete"], data["stage"])
	}
	spec, err = env.specFor("demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(spec.Plugins) != 1 {
		t.Errorf("la lista se duplicó: %v", spec.Plugins)
	}
	reg = readRegister(t, dataDir, spec)
	if reg.Len() != 1 {
		t.Errorf("la procedencia se duplicó: %+v", reg.Records)
	}
	assertSinTemporales(t, pluginDir(dataDir))
}

// TestPluginInstallDependenciaObligatoria: sin --allow-missing-deps la
// dependencia ausente bloquea; con el flag queda como aviso y NUNCA se instala
// la dependencia sola.
func TestPluginInstallDependenciaObligatoria(t *testing.T) {
	env, out, dataDir := pluginTestEnv(t)
	f := newMktFixture(t)
	f.addProject(mktDemoProject(), f.version("5.5.0", "2025-06-01T00:00:00Z", "release",
		[]string{"paper"}, []string{"1.21.4"}, map[string]string{"sha512": f.sha512},
		relDep("P2Otro", "OtroPlugin")))
	withFixtureCatalog(t, f)

	// 1. Sin el flag: error explícito y nada instalado. El motivo nombra la
	// dependencia que falta (el instalador no devuelve la resolución cuando
	// falla, así que la lista detallada solo está disponible con el aviso).
	if err := runMarketCmd(t, newPluginInstallCmd(env), "viaversion", "--allow-http", "--allow-loopback"); err == nil {
		t.Fatal("se esperaba error por dependencia obligatoria ausente")
	}
	doc := marketDoc(t, out, "plugin install")
	data := marketData(t, doc)
	if data["resolution_complete"] != false || data["stage"] != "resolucion" {
		t.Errorf("resolution_complete/stage = %v/%v", data["resolution_complete"], data["stage"])
	}
	problems, _ := data["problems"].([]any)
	if len(problems) != 1 {
		t.Fatalf("problems = %v", data["problems"])
	}
	p0, _ := problems[0].(map[string]any)
	if p0["reason"] != "dependencia_no_satisfecha" {
		t.Errorf("problems[0].reason = %v", p0["reason"])
	}
	if !strings.Contains(anyString(p0["message"]), "OtroPlugin") {
		t.Errorf("el motivo debe nombrar la dependencia: %v", p0["message"])
	}
	if msg, _ := doc["message"].(string); !strings.Contains(msg, "dependencias obligatorias") {
		t.Errorf("message = %v", doc["message"])
	}
	assertSinArchivos(t, pluginDir(dataDir))
	assertSinProcedencia(t, dataDir)

	// 2. Con --allow-missing-deps: aviso (exit 1) y se instala SOLO el plugin.
	out.Reset()
	if err := runMarketCmd(t, newPluginInstallCmd(env), "viaversion",
		"--allow-http", "--allow-loopback", "--allow-missing-deps"); err == nil {
		t.Fatal("se esperaba aviso (exit_code 1)")
	}
	doc = marketDoc(t, out, "plugin install")
	if doc["exit_code"] != float64(1) || doc["ok"] != false {
		t.Errorf("documento inesperado: %v", doc)
	}
	data = marketData(t, doc)
	if data["resolution_complete"] != true {
		t.Errorf("resolution_complete = %v", data["resolution_complete"])
	}
	warnings, _ := data["warnings"].([]any)
	if len(warnings) == 0 {
		t.Errorf("falta el aviso de dependencias: %v", data)
	}
	if !strings.Contains(strings.Join(anyStrings(warnings), " "), "dependencias obligatorias") {
		t.Errorf("warnings = %v", warnings)
	}
	entries, err := os.ReadDir(pluginDir(dataDir))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "ViaVersion-5.5.0.jar" {
		t.Errorf("solo debería estar el plugin principal: %v", entries)
	}
	spec, err := env.specFor("demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(spec.Plugins) != 1 || spec.Plugins[0] != "ViaVersion-5.5.0.jar" {
		t.Errorf("la dependencia NO debe aparecer en la lista: %v", spec.Plugins)
	}
}

// anyString convierte un valor JSON cualquiera a texto.
func anyString(v any) string {
	s, _ := v.(string)
	return s
}

// anyStrings convierte una lista JSON de cadenas a []string.
func anyStrings(list []any) []string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// --- `plugin updates` --------------------------------------------------------

// TestPluginUpdatesDetectaYNoModificaElArchivo: la comprobación informa de la
// versión nueva pero NO descarga, NO instala y NO toca el archivo; solo con
// --persist-targets anota la versión objetivo.
func TestPluginUpdatesDetectaYNoModificaElArchivo(t *testing.T) {
	env, out, dataDir := pluginTestEnv(t)
	f := newMktFixture(t)
	f.addProject(mktDemoProject(), f.version("5.5.0", "2025-06-01T00:00:00Z", "release",
		[]string{"paper"}, []string{"1.21.4"}, map[string]string{"sha512": f.sha512}))
	withFixtureCatalog(t, f)

	// Instalación previa (versión antigua) + un archivo sin procedencia.
	dir := pluginDir(dataDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	const (
		oldName = "ViaVersion-5.0.0.jar"
		rawName = "SinProcedencia.jar"
	)
	oldPath := filepath.Join(dir, oldName)
	if err := os.WriteFile(oldPath, f.jar, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	seedInstalled(t, env, dataDir, oldName, plugins.Record{
		Name: oldName, Version: "5.0.0", SHA256: f.sha256, SizeBytes: int64(len(f.jar)),
		Loader: "paper", GameVersion: "1.21.4",
		Origin: plugins.Origin{
			Source: "modrinth", ProjectID: "P1OZGk5p", ProjectSlug: "viaversion",
			ProjectName: "ViaVersion", VersionID: "id-5.0.0", Channel: "release", Kind: "plugin",
		},
	})
	if err := os.WriteFile(filepath.Join(dir, rawName), f.jar, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	addPluginName(t, env, rawName) // instalado a mano: sin procedencia
	spec, err := env.specFor("demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	before, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	fiBefore, err := os.Stat(oldPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	modBefore := fiBefore.ModTime().String()

	if err := runMarketCmd(t, newPluginUpdatesCmd(env)); err == nil {
		t.Fatal("se esperaba aviso (exit_code 1) por actualización disponible")
	}
	doc := marketDoc(t, out, "plugin updates")
	if doc["ok"] != false || doc["exit_code"] != float64(1) {
		t.Fatalf("documento inesperado: %v", doc)
	}
	data := marketData(t, doc)
	if data["available"] != float64(1) || data["checked"] != float64(2) {
		t.Errorf("available/checked = %v/%v", data["available"], data["checked"])
	}
	if data["installed_modified"] != false {
		t.Errorf("installed_modified = %v", data["installed_modified"])
	}
	if note, _ := data["note"].(string); !strings.Contains(note, "no descarga ni modifica") {
		t.Errorf("note = %v", data["note"])
	}
	byName := map[string]map[string]any{}
	updates, _ := data["updates"].([]any)
	for _, raw := range updates {
		m, _ := raw.(map[string]any)
		if name, ok := m["name"].(string); ok {
			byName[name] = m
		}
	}
	up := byName[oldName]
	if up == nil {
		t.Fatalf("falta la entrada de %s: %v", oldName, data["updates"])
	}
	if up["status"] != "actualizacion_disponible" || up["version"] != "5.0.0" || up["target_version"] != "5.5.0" {
		t.Errorf("actualización = %v", up)
	}
	if up["compatibility"] != "compatible" || up["publication"] == "" {
		t.Errorf("compatibilidad/publicación = %v/%v", up["compatibility"], up["publication"])
	}
	unk := byName[rawName]
	if unk == nil || unk["status"] != "desconocida" {
		t.Errorf("sin procedencia el estado debe ser desconocida: %v", unk)
	}

	// El archivo instalado NO cambia.
	assertFileUnchanged(t, oldPath, before, modBefore)

	// La versión instalada sigue siendo la antigua y no se anotó objetivo.
	reg := readRegister(t, dataDir, spec)
	rec, ok := reg.Get(oldName)
	if !ok || rec.Version != "5.0.0" || rec.TargetVersion != "" {
		t.Errorf("registro tras comprobar = %+v", rec)
	}

	// Con --persist-targets solo se anota la versión objetivo.
	out.Reset()
	if err := runMarketCmd(t, newPluginUpdatesCmd(env), "--persist-targets"); err == nil {
		t.Fatal("se esperaba aviso (exit_code 1)")
	}
	doc = marketDoc(t, out, "plugin updates")
	data = marketData(t, doc)
	if data["persisted_targets"] != true {
		t.Errorf("persisted_targets = %v", data["persisted_targets"])
	}
	reg = readRegister(t, dataDir, spec)
	rec, ok = reg.Get(oldName)
	if !ok {
		t.Fatalf("falta la entrada en el registro: %+v", reg.Records)
	}
	if rec.TargetVersion != "5.5.0" {
		t.Errorf("target_version = %q, quiero 5.5.0", rec.TargetVersion)
	}
	if rec.Version != "5.0.0" {
		t.Errorf("la versión INSTALADA no debe cambiar: %q", rec.Version)
	}
	if rec.SHA256 != f.sha256 {
		t.Errorf("el checksum registrado no debe cambiar: %q", rec.SHA256)
	}
	assertFileUnchanged(t, oldPath, before, modBefore)
}

// TestPluginUpdatesSinInstalaciones: sin nada instalado la comprobación es un
// éxito vacío, no un error.
func TestPluginUpdatesSinInstalaciones(t *testing.T) {
	env, out, _ := pluginTestEnv(t)
	f := newMktFixture(t)
	withFixtureCatalog(t, f)

	if err := runMarketCmd(t, newPluginUpdatesCmd(env)); err != nil {
		t.Fatalf("plugin updates: %v", err)
	}
	doc := marketDoc(t, out, "plugin updates")
	if doc["ok"] != true || doc["exit_code"] != float64(0) {
		t.Errorf("documento inesperado: %v", doc)
	}
	data := marketData(t, doc)
	if data["checked"] != float64(0) || data["available"] != float64(0) {
		t.Errorf("checked/available = %v/%v", data["checked"], data["available"])
	}
}
