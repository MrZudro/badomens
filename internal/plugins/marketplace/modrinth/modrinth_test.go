package modrinth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/plugins/marketplace"
)

// petición capturada por el servidor de pruebas, para comprobar filtros y
// cabeceras sin red real.
type captured struct {
	path   string
	query  string
	ua     string
	accept string
}

// testServer monta un servidor que sirve los fixtures de testdata y registra la
// última petición recibida.
func testServer(t *testing.T) (*Client, *captured) {
	t.Helper()
	cap := &captured{}
	mux := http.NewServeMux()
	serve := func(route, fixture string) {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			raw, err := os.ReadFile(filepath.Join("testdata", fixture))
			if err != nil {
				t.Errorf("fixture %s: %v", fixture, err)
				http.Error(w, "fixture ausente", http.StatusInternalServerError)
				return
			}
			cap.path, cap.query = r.URL.Path, r.URL.RawQuery
			cap.ua, cap.accept = r.Header.Get("User-Agent"), r.Header.Get("Accept")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(raw)
		})
	}
	serve("/search", "search_two_hits.json")
	serve("/project/viaversion", "project_viaversion.json")
	serve("/project/borrador", "project_draft.json")
	serve("/project/viaversion/version", "versions_paper.json")
	serve("/project/lithium/version", "versions_other_loader.json")
	serve("/project/sincheksum/version", "versions_no_hashes.json")
	serve("/project/falla/version", "versions_bad_schema.json")
	mux.HandleFunc("/project/lithium", func(w http.ResponseWriter, r *http.Request) {
		raw, err := os.ReadFile(filepath.Join("testdata", "project_lithium.json"))
		if err != nil {
			http.Error(w, "fixture ausente", http.StatusInternalServerError)
			return
		}
		cap.path, cap.query = r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	})
	// /proyecto/caido redirige a una ruta que no existe: se comprueba que el
	// transporte sigue la redirección y clasifica el 404.
	mux.HandleFunc("/project/caido", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/no-existe", http.StatusFound)
	})
	mux.HandleFunc("/project/error500", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	httpClient := engines.DefaultClient()
	httpClient.MaxAttempts = 1 // sin reintentos: las pruebas deben ser rápidas
	client := New(httpClient)
	client.BaseURL = srv.URL
	return client, cap
}

func TestIdentidad(t *testing.T) {
	c := New(nil)
	if c.ID() != marketplace.SourceModrinth {
		t.Errorf("ID = %q", c.ID())
	}
	if c.Label() != "Modrinth" {
		t.Errorf("Label = %q", c.Label())
	}
	if _, ok := interface{}(c).(marketplace.Source); !ok {
		t.Error("Client no implementa marketplace.Source")
	}
}

// TestSearchNoAfirmaCompatibilidad: un resultado de búsqueda nunca se marca
// compatible, ni siquiera cuando el proveedor devuelve la versión de Minecraft y
// pone "fabric" entre las categorías. La búsqueda de Modrinth no declara
// loaders, así que el veredicto es Unknown.
func TestSearchNoAfirmaCompatibilidad(t *testing.T) {
	c, cap := testServer(t)
	res, err := c.Search(context.Background(), marketplace.SearchQuery{
		Text: "lithium",
		Env:  marketplace.Environment{Engine: "fabric", Loader: "fabric", GameVersion: "1.21.1"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Projects) != 2 {
		t.Fatalf("proyectos = %d, quiero 2", len(res.Projects))
	}
	if res.Total != 2 {
		t.Errorf("Total = %d, quiero 2", res.Total)
	}
	lithium := res.Projects[0]
	if lithium.Slug != "lithium" || lithium.Kind != marketplace.KindMod {
		t.Errorf("primer resultado = %+v", lithium)
	}
	if len(lithium.Loaders) != 0 {
		t.Errorf("Loaders = %v, quiero vacío: la búsqueda no declara loaders", lithium.Loaders)
	}
	if lithium.Compat != marketplace.Unknown {
		t.Errorf("Compat = %q, quiero %q", lithium.Compat, marketplace.Unknown)
	}
	if !strings.Contains(lithium.CompatReason, "no declara loaders") {
		t.Errorf("CompatReason = %q", lithium.CompatReason)
	}
	// Las categorías SÍ se conservan, pero no como prueba de compatibilidad.
	if len(lithium.Categories) == 0 {
		t.Error("se perdieron las categorías")
	}
	if res.Projects[1].Kind != marketplace.KindModpack {
		t.Errorf("segundo resultado kind = %q", res.Projects[1].Kind)
	}
	// Los resultados de búsqueda no traen estado de publicación.
	if res.Projects[0].Published {
		t.Error("un resultado de búsqueda no puede declararse publicado sin datos")
	}
	if lbl := marketplace.PublishedLabel(lithium.Published, lithium.Status); lbl != "desconocido" {
		t.Errorf("etiqueta de publicación = %q", lbl)
	}
	if cap.ua == "" || !strings.Contains(cap.ua, "badomens") || !strings.Contains(cap.ua, "github.com/MrZudro") {
		t.Errorf("User-Agent sin contacto: %q", cap.ua)
	}
}

// TestSearchFiltroEnServidor: cuando se pide filtrar por entorno, el adaptador
// manda facets de tipo de proyecto, versiones y categorías, en ese orden.
func TestSearchFiltroEnServidor(t *testing.T) {
	c, cap := testServer(t)
	_, err := c.Search(context.Background(), marketplace.SearchQuery{
		Text:      "viaversion",
		Kind:      marketplace.KindPlugin,
		Env:       marketplace.Environment{Engine: "paper", Loader: "paper", GameVersion: "1.21.1"},
		FilterEnv: true,
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	q := cap.query
	for _, want := range []string{"facets", "project_type%3Aplugin", "versions%3A1.21.1", "categories%3Apaper", "limit=5"} {
		if !strings.Contains(q, want) {
			t.Errorf("consulta %q no contiene %q", q, want)
		}
	}
}

// TestSearchSinFiltro: sin FilterEnv no se manda ningún facet de entorno.
func TestSearchSinFiltro(t *testing.T) {
	c, cap := testServer(t)
	if _, err := c.Search(context.Background(), marketplace.SearchQuery{Text: "lithium"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if strings.Contains(cap.query, "facets") {
		t.Errorf("no debía mandar facets: %q", cap.query)
	}
}

// TestProjectDeclaraCompatibilidad: el proyecto sí declara loaders y versiones;
// ahí la compatibilidad es verificable (a favor o en contra).
func TestProjectDeclaraCompatibilidad(t *testing.T) {
	c, _ := testServer(t)
	proj, err := c.Project(context.Background(), "viaversion")
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if proj.Kind != marketplace.KindMod || proj.ID != "P1OZGk5p" {
		t.Errorf("proyecto = %+v", proj)
	}
	if !proj.Published {
		t.Errorf("el proyecto debería estar publicado (status %q)", proj.Status)
	}
	if proj.URL != "https://modrinth.com/project/viaversion" {
		t.Errorf("URL = %q", proj.URL)
	}

	ok, reason := proj.Compatibility(marketplace.Environment{Engine: "paper", Loader: "paper", GameVersion: "1.21.1"})
	if ok != marketplace.Compatible {
		t.Errorf("compat paper/1.21.1 = %q (%s)", ok, reason)
	}
	no, reason := proj.Compatibility(marketplace.Environment{Engine: "paper", Loader: "paper", GameVersion: "1.19.4"})
	if no != marketplace.Incompatible {
		t.Errorf("compat paper/1.19.4 = %q (%s)", no, reason)
	}
	if !strings.Contains(reason, "1.19.4") {
		t.Errorf("motivo poco informativo: %q", reason)
	}
	unknown, _ := proj.Compatibility(marketplace.Environment{Engine: "paper", Loader: "paper"})
	if unknown != marketplace.Unknown {
		t.Errorf("sin versión de Minecraft la compatibilidad debe ser desconocida, no %q", unknown)
	}
}

// TestProjectNoPublicado: un estado distinto de approved/unlisted no se
// considera publicado.
func TestProjectNoPublicado(t *testing.T) {
	c, _ := testServer(t)
	proj, err := c.Project(context.Background(), "borrador")
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if proj.Published {
		t.Error("un proyecto en draft no está publicado")
	}
	if proj.Status != "draft" {
		t.Errorf("Status = %q", proj.Status)
	}
}

// TestProjectNoExiste: 404 ⇒ error tipado not_found con motivo estable.
func TestProjectNoExiste(t *testing.T) {
	c, _ := testServer(t)
	_, err := c.Project(context.Background(), "esto-no-existe")
	if err == nil {
		t.Fatal("se esperaba error")
	}
	if !errx.Is(err, errx.CodeNotFound) {
		t.Errorf("código = %v", err)
	}
	code, _, _ := errx.From(err)
	if code != errx.CodeNotFound {
		t.Errorf("code = %q", code)
	}
	if got := reason(t, err); got != marketplace.ReasonNotFound {
		t.Errorf("reason = %q", got)
	}
}

// TestProjectRedireccionA404: el transporte sigue la redirección y clasifica el
// 404 igual.
func TestProjectRedireccionA404(t *testing.T) {
	c, _ := testServer(t)
	_, err := c.Project(context.Background(), "caido")
	if err == nil {
		t.Fatal("se esperaba error")
	}
	if !errx.Is(err, errx.CodeNotFound) {
		t.Errorf("se esperaba not_found, got %v", err)
	}
}

// TestProjectServidor500: un 5xx se clasifica como proveedor no disponible.
func TestProjectServidor500(t *testing.T) {
	c, _ := testServer(t)
	_, err := c.Project(context.Background(), "error500")
	if err == nil {
		t.Fatal("se esperaba error")
	}
	if !errx.Is(err, errx.CodeUnavailable) {
		t.Errorf("se esperaba unavailable, got %v", err)
	}
	if got := reason(t, err); got != marketplace.ReasonUpstream {
		t.Errorf("reason = %q", got)
	}
	if d := errx.Details(err); d["http_status"] != 500 {
		t.Errorf("http_status = %v", d["http_status"])
	}
}

// TestVersionsFiltraYTraduce comprueba el filtro por loader y versión de
// Minecraft (que Modrinth aplica en el servidor) y la traducción del esquema.
func TestVersionsFiltraYTraduce(t *testing.T) {
	c, cap := testServer(t)
	versions, err := c.Versions(context.Background(), "viaversion", marketplace.VersionFilter{
		Loaders:      []string{"paper"},
		GameVersions: []string{"1.21.1"},
	})
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("versiones = %d, quiero 3", len(versions))
	}
	q := cap.query
	if !strings.Contains(q, "loaders=%5B%22paper%22%5D") {
		t.Errorf("lack of loaders JSON array: %q", q)
	}
	if !strings.Contains(q, "game_versions=%5B%221.21.1%22%5D") {
		t.Errorf("falta game_versions como array JSON: %q", q)
	}

	v := versions[0]
	if v.ID != "VvREL5x1" || v.Number != "5.5.0" || v.Channel != "release" {
		t.Errorf("versión = %+v", v)
	}
	if !v.Published {
		t.Error("la versión está listada: debería constar como publicada")
	}
	if len(v.Loaders) != 2 || v.Loaders[0] != "folia" {
		t.Errorf("Loaders = %v", v.Loaders)
	}
	file, ok := v.PrimaryFile()
	if !ok {
		t.Fatal("sin archivo principal")
	}
	if !file.Primary || file.Name != "ViaVersion-5.5.0.jar" {
		t.Errorf("archivo principal = %+v", file)
	}
	if _, hasSHA256 := file.Hash(marketplace.HashSHA256); hasSHA256 {
		t.Error("Modrinth no publica sha256: no debe inventarse")
	}
	if _, hasSHA512 := file.Hash(marketplace.HashSHA512); !hasSHA512 {
		t.Error("falta el sha512 publicado")
	}
	if !file.Verifiable() {
		t.Error("un sha512 permite verificar: debería ser verificable")
	}
	if v.Prerelease() {
		t.Error("release no es preliminar")
	}

	// Dependencias traducidas con su tipo.
	kinds := map[string]marketplace.DependencyKind{}
	for _, d := range v.Dependencies {
		kinds[d.ProjectID] = d.Kind
	}
	if kinds["YlKdE5VK"] != marketplace.DepRequired ||
		kinds["TbHIxhx5"] != marketplace.DepOptional ||
		kinds["NpvuJQoq"] != marketplace.DepEmbedded {
		t.Errorf("dependencias = %+v", v.Dependencies)
	}

	// La segunda versión es preliminar y la tercera está archivada.
	if versions[1].Channel != "beta" || !versions[1].Prerelease() {
		t.Errorf("segunda versión = %+v", versions[1])
	}
	if versions[2].Published {
		t.Error("una versión archivada no está publicada")
	}
}

// TestVersionsSinArchivoCompatibleConElLoader: el adaptador devuelve lo que el
// proveedor publica, pero la compatibilidad declarada es Incompatible, así que
// el resolvedor la descarta. Nunca se convierte en compatible.
func TestVersionsSinArchivoCompatibleConElLoader(t *testing.T) {
	c, _ := testServer(t)
	versions, err := c.Versions(context.Background(), "lithium", marketplace.VersionFilter{
		Loaders:      []string{"paper"},
		GameVersions: []string{"1.21.1"},
	})
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("versiones = %d", len(versions))
	}
	compat, reason := versions[0].Compatibility(marketplace.Environment{Engine: "paper", Loader: "paper", GameVersion: "1.21.1"})
	if compat != marketplace.Incompatible {
		t.Errorf("compat = %q, quiero incompatible", compat)
	}
	if !strings.Contains(reason, "neoforge") {
		t.Errorf("motivo = %q", reason)
	}
}

// TestVersionsSinChecksumUtilizable: un archivo que solo publica sha1 queda SIN
// verificación disponible y así se informa (no se finge).
func TestVersionsSinChecksumUtilizable(t *testing.T) {
	c, _ := testServer(t)
	versions, err := c.Versions(context.Background(), "sincheksum", marketplace.VersionFilter{Loaders: []string{"paper"}})
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("versiones = %d", len(versions))
	}
	file, _ := versions[0].PrimaryFile()
	if file.Verifiable() {
		t.Error("sha1 no basta para verificar integridad")
	}
	if _, ok := file.StrongestHash(); ok {
		t.Error("no hay checksum fuerte: StrongestHash debe fallar")
	}
	if _, ok := file.Hash(marketplace.HashSHA1); !ok {
		t.Error("el sha1 publicado se conserva para informarlo, aunque no se use")
	}
}

// TestVersionsEsquemaInvalido: faltan claves obligatorias del esquema ⇒ error
// tipado de respuesta inválida (no se devuelven versiones a medias).
func TestVersionsEsquemaInvalido(t *testing.T) {
	c, _ := testServer(t)
	_, err := c.Versions(context.Background(), "falla", marketplace.VersionFilter{})
	if err == nil {
		t.Fatal("se esperaba error")
	}
	if !errx.Is(err, errx.CodeUnavailable) {
		t.Errorf("código = %v", err)
	}
	if got := reason(t, err); got != marketplace.ReasonRespuestaInvalida {
		t.Errorf("reason = %q", got)
	}
}

// TestVersionsAcotaResultados: el tope de versiones es nuestro y se respeta.
func TestVersionsAcotaResultados(t *testing.T) {
	c, _ := testServer(t)
	versions, err := c.Versions(context.Background(), "viaversion", marketplace.VersionFilter{Limit: 2})
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if len(versions) != 2 {
		t.Errorf("versiones = %d, quiero 2", len(versions))
	}
}

// TestProjectVacio: sin referencia no se consulta nada.
func TestProjectVacio(t *testing.T) {
	c, _ := testServer(t)
	if _, err := c.Project(context.Background(), "  "); !errx.Is(err, errx.CodeUsage) {
		t.Errorf("se esperaba error_usage, got %v", err)
	}
}

// reason extrae el motivo estable de un error tipado.
func reason(t *testing.T, err error) string {
	t.Helper()
	d := errx.Details(err)
	if d == nil {
		t.Fatalf("sin detalles en %v", err)
	}
	r, _ := d["reason"].(string)
	return r
}
