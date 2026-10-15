package hangar

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

// fixtureServer sirve los fixtures de testdata en la forma de la API de Hangar.
func fixtureServer(t *testing.T) (*Client, *string) {
	t.Helper()
	cap := new(string)
	mux := http.NewServeMux()
	serve := func(route, fixture string) {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			raw, err := os.ReadFile(filepath.Join("testdata", fixture))
			if err != nil {
				t.Errorf("fixture %s: %v", fixture, err)
				http.Error(w, "fixture ausente", http.StatusInternalServerError)
				return
			}
			*cap = r.URL.RawQuery
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(raw)
		})
	}
	serve("/projects", "search_projects.json")
	serve("/projects/fancyglow", "project_fancyglow.json")
	serve("/projects/fancyglow/versions", "versions_external.json")
	serve("/projects/WirelessRedstoneTrigger/versions", "versions_internal.json")
	serve("/projects/SoloVelocity/versions", "versions_other_platform.json")
	serve("/projects/falla/versions", "versions_bad_schema.json")
	mux.HandleFunc("/projects/no-existe", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Unknown value","isHangarApiException":true,"httpError":{"statusCode":404}}`))
	})
	mux.HandleFunc("/proyectos/caido", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/no-existe", http.StatusFound)
	})
	mux.HandleFunc("/projects/error500", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	httpClient := engines.DefaultClient()
	httpClient.MaxAttempts = 1
	client := New(httpClient)
	client.BaseURL = srv.URL
	return client, cap
}

func TestIdentidad(t *testing.T) {
	c := New(nil)
	if c.ID() != marketplace.SourceHangar {
		t.Errorf("ID = %q", c.ID())
	}
	if c.Label() == "" {
		t.Error("Label vacío")
	}
	if _, ok := interface{}(c).(marketplace.Source); !ok {
		t.Error("Client no implementa marketplace.Source")
	}
}

// TestSearchTraducePlataformas: Hangar publica las plataformas de cada proyecto,
// así que aquí la compatibilidad SÍ se calcula; las versiones de Minecraft solo
// se conocen al consultar las versiones, así que sin ese dato el veredicto queda
// en Unknown.
func TestSearchTraducePlataformas(t *testing.T) {
	c, cap := fixtureServer(t)
	res, err := c.Search(context.Background(), marketplace.SearchQuery{
		Text:      "glow",
		Env:       marketplace.Environment{Engine: "paper", Loader: "paper", GameVersion: "1.21"},
		FilterEnv: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Projects) != 2 || res.Total != 2 {
		t.Fatalf("resultados = %d (total %d)", len(res.Projects), res.Total)
	}
	glow := res.Projects[0]
	if glow.Kind != marketplace.KindPlugin {
		t.Errorf("Kind = %q, Hangar solo publica plugins", glow.Kind)
	}
	if len(glow.Loaders) != 1 || glow.Loaders[0] != "PAPER" {
		t.Errorf("Loaders = %v", glow.Loaders)
	}
	if !glow.Published {
		t.Error("visibility public debería contar como publicado")
	}
	if glow.URL == "" || !strings.Contains(glow.URL, "fancyglow") {
		t.Errorf("URL = %q", glow.URL)
	}
	if len(glow.GameVersions) == 0 {
		t.Error("faltan las versiones de Minecraft declaradas por plataforma")
	}
	// El segundo proyecto es unlisted: no está publicado.
	if res.Projects[1].Published {
		t.Error("visibility unlisted no está publicado")
	}
	// El filtro de entorno (plataforma) se manda en la consulta.
	if !strings.Contains(*cap, "platform=PAPER") {
		t.Errorf("consulta sin filtro de plataforma: %q", *cap)
	}
}

func TestSearchSinFiltroNoMandaPlataforma(t *testing.T) {
	c, cap := fixtureServer(t)
	if _, err := c.Search(context.Background(), marketplace.SearchQuery{Text: "glow"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if strings.Contains(*cap, "platform=") {
		t.Errorf("no debía filtrar por plataforma: %q", *cap)
	}
}

func TestProjectFancyglow(t *testing.T) {
	c, _ := fixtureServer(t)
	proj, err := c.Project(context.Background(), "fancyglow")
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if proj.ID != "1950" || proj.Slug != "fancyglow" || proj.Author != "hhitt" {
		t.Errorf("proyecto = %+v", proj)
	}
	if !proj.Published {
		t.Error("debería estar publicado")
	}
	if proj.License != "GPL" {
		t.Errorf("License = %q", proj.License)
	}
	compat, _ := proj.Compatibility(marketplace.Environment{Engine: "paper", Loader: "paper", GameVersion: "1.21"})
	if compat != marketplace.Compatible {
		t.Errorf("compat = %q", compat)
	}
	// Hangar no publica plugins de Fabric: su plataforma no encaja.
	if compat, _ := proj.Compatibility(marketplace.Environment{Engine: "fabric", Loader: "fabric", GameVersion: "1.21"}); compat != marketplace.Incompatible {
		t.Errorf("compat fabric = %q, quiero incompatible", compat)
	}
}

func TestProjectNoExiste(t *testing.T) {
	c, _ := fixtureServer(t)
	_, err := c.Project(context.Background(), "noexiste")
	if err == nil {
		t.Fatal("se esperaba error")
	}
	if !errx.Is(err, errx.CodeNotFound) {
		t.Errorf("se esperaba not_found, got %v", err)
	}
	if got := detailsReason(t, err); got != marketplace.ReasonNotFound {
		t.Errorf("reason = %q", got)
	}
}

func TestProjectVacio(t *testing.T) {
	c, _ := fixtureServer(t)
	if _, err := c.Project(context.Background(), ""); !errx.Is(err, errx.CodeUsage) {
		t.Errorf("se esperaba error_usage, got %v", err)
	}
}

// TestVersionsInternas: archivo alojado por Hangar con sha256 ⇒ verificable.
func TestVersionsInternas(t *testing.T) {
	c, _ := fixtureServer(t)
	versions, err := c.Versions(context.Background(), "WirelessRedstoneTrigger", marketplace.VersionFilter{
		Loaders:      []string{"paper"},
		GameVersions: []string{"1.21"},
	})
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("versiones = %d, quiero 2", len(versions))
	}
	v := versions[0]
	if v.ID != "28830" || v.Number != "2.0" || v.Channel != "release" {
		t.Errorf("versión = %+v", v)
	}
	if !v.Published || !strings.Contains(v.Status, "reviewed") {
		t.Errorf("estado = %q publicado=%v", v.Status, v.Published)
	}
	if len(v.GameVersions) != 1 || v.GameVersions[0] != "1.21" {
		t.Errorf("GameVersions = %v", v.GameVersions)
	}
	file, ok := v.PrimaryFile()
	if !ok {
		t.Fatal("sin archivo principal")
	}
	if file.External {
		t.Error("el archivo está alojado por Hangar: no es externo")
	}
	if file.Size != 13042 {
		t.Errorf("Size = %d", file.Size)
	}
	sha, has := file.Hash(marketplace.HashSHA256)
	if !has || len(sha) != 64 {
		t.Errorf("sha256 = %q (%v)", sha, has)
	}
	if !file.Verifiable() {
		t.Error("con sha256 el artefacto es verificable")
	}

	// La versión antigua declara una dependencia obligatoria.
	old := versions[1]
	if len(old.Dependencies) != 1 || old.Dependencies[0].Kind != marketplace.DepRequired {
		t.Errorf("dependencias = %+v", old.Dependencies)
	}
	if old.Dependencies[0].Name != "PlaceholderAPI" {
		t.Errorf("nombre de dependencia = %q", old.Dependencies[0].Name)
	}
}

// TestVersionsExternas: cuando Hangar no aloja el archivo, no hay tamaño ni
// checksum. Se informa de que NO hay verificación disponible.
func TestVersionsExternas(t *testing.T) {
	c, _ := fixtureServer(t)
	versions, err := c.Versions(context.Background(), "fancyglow", marketplace.VersionFilter{Loaders: []string{"paper"}})
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("versiones = %d", len(versions))
	}
	file, ok := versions[0].PrimaryFile()
	if !ok {
		t.Fatal("sin archivo principal")
	}
	if !file.External {
		t.Error("el archivo lo aloja un tercero: debe marcarse como externo")
	}
	if file.Size != 0 {
		t.Errorf("Size = %d, Hangar no lo publica para archivos externos", file.Size)
	}
	if file.Verifiable() {
		t.Error("sin checksum publicado no hay verificación disponible")
	}
	if !strings.HasSuffix(file.Name, ".jar") {
		t.Errorf("nombre derivado de la URL = %q", file.Name)
	}
}

// TestVersionsFiltraPlataforma: una versión que solo publica VELOCITY no es una
// opción para un servidor Paper.
func TestVersionsFiltraPlataforma(t *testing.T) {
	c, _ := fixtureServer(t)
	paper, err := c.Versions(context.Background(), "SoloVelocity", marketplace.VersionFilter{Loaders: []string{"paper"}})
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if len(paper) != 0 {
		t.Errorf("un plugin de VELOCITY no debe ofrecerse para paper: %+v", paper)
	}
	velocity, err := c.Versions(context.Background(), "SoloVelocity", marketplace.VersionFilter{Loaders: []string{"velocity"}})
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if len(velocity) != 1 {
		t.Fatalf("versiones para velocity = %d", len(velocity))
	}
	if velocity[0].Loaders[0] != "VELOCITY" {
		t.Errorf("Loaders = %v", velocity[0].Loaders)
	}
}

// TestVersionsFiltraVersionDeMinecraft: las versiones de Minecraft se filtran en
// el cliente, sobre lo que declara cada versión.
func TestVersionsFiltraVersionDeMinecraft(t *testing.T) {
	c, _ := fixtureServer(t)
	got, err := c.Versions(context.Background(), "WirelessRedstoneTrigger", marketplace.VersionFilter{
		Loaders:      []string{"paper"},
		GameVersions: []string{"1.20.6"},
	})
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if len(got) != 1 || got[0].Number != "1.0" {
		t.Fatalf("versiones = %+v, quiero solo la 1.0", got)
	}
	none, err := c.Versions(context.Background(), "WirelessRedstoneTrigger", marketplace.VersionFilter{
		Loaders:      []string{"paper"},
		GameVersions: []string{"1.7.10"},
	})
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("no hay versión para 1.7.10: %+v", none)
	}
}

func TestVersionsEsquemaInvalido(t *testing.T) {
	c, _ := fixtureServer(t)
	_, err := c.Versions(context.Background(), "falla", marketplace.VersionFilter{})
	if err == nil {
		t.Fatal("se esperaba error")
	}
	if got := detailsReason(t, err); got != marketplace.ReasonRespuestaInvalida {
		t.Errorf("reason = %q", got)
	}
}

func TestProjectServidor500YRedireccion(t *testing.T) {
	c, _ := fixtureServer(t)
	if _, err := c.Project(context.Background(), "error500"); !errx.Is(err, errx.CodeUnavailable) {
		t.Errorf("500: se esperaba unavailable, got %v", err)
	}
	if _, err := c.Project(context.Background(), "caido"); err == nil {
		t.Error("la redirección a una ruta inexistente debe fallar")
	}
}

// platformFor y platformsFor traducen el vocabulario de badomens al de Hangar.
func TestTraduccionDePlataformas(t *testing.T) {
	if platformFor("paper") != PlatformPaper || platformFor("fabric") != "" {
		t.Errorf("platformFor = %q / %q", platformFor("paper"), platformFor("fabric"))
	}
	got := platformsFor([]string{"paper", "Paper", "velocity", "themodloader"})
	if len(got) != 2 || got[0] != "PAPER" || got[1] != "VELOCITY" {
		t.Errorf("platformsFor = %v", got)
	}
}

// detailsReason extrae el motivo estable de un error tipado.
func detailsReason(t *testing.T, err error) string {
	t.Helper()
	d := errx.Details(err)
	if d == nil {
		t.Fatalf("sin detalles en %v", err)
	}
	r, _ := d["reason"].(string)
	return r
}
