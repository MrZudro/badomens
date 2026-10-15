package engines

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ===========================================================================
// Utilidades comunes de las pruebas de proveedores (Paper y Vanilla)
// ===========================================================================

// readFixture carga un fixture real de internal/engines/testdata (respuestas ya
// capturadas de los endpoints oficiales). Las pruebas NUNCA salen a la red.
func readFixture(t *testing.T, parts ...string) []byte {
	t.Helper()
	path := filepath.Join(append([]string{"testdata"}, parts...)...)
	raw, err := os.ReadFile(path) // #nosec G304 -- fixture de test bajo testdata del propio paquete
	if err != nil {
		t.Fatalf("no se pudo leer el fixture %s: %v", path, err)
	}
	return raw
}

// staticTestClient devuelve un cliente determinista para las pruebas: un solo
// intento (un 5xx no debe alargar la suite con backoff) y timeout corto.
func staticTestClient() *Client {
	return &Client{
		HTTP:        &http.Client{Timeout: 2 * time.Second},
		UserAgent:   "badomens-test",
		MaxAttempts: 1,
	}
}

// fakeRoute es una respuesta fija de fakeCatalog (status 0 = 200).
type fakeRoute struct {
	body   string
	status int
}

// fakeCatalog es un servidor de pruebas que sirve respuestas fijas por ruta y
// anota las peticiones recibidas, para poder comprobar qué endpoints se
// consultan (y que no se consulta ninguno de más).
type fakeCatalog struct {
	server *httptest.Server
	mu     sync.Mutex
	hits   []string
	routes map[string]fakeRoute
}

func newFakeCatalog(t *testing.T) *fakeCatalog {
	t.Helper()
	f := &fakeCatalog{routes: map[string]fakeRoute{}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hits = append(f.hits, r.URL.Path)
		route, ok := f.routes[r.URL.Path]
		f.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if route.status != 0 {
			w.WriteHeader(route.status)
		}
		_, _ = w.Write([]byte(route.body))
	}))
	t.Cleanup(f.server.Close)
	return f
}

// route registra una respuesta (status 0 = 200).
func (f *fakeCatalog) route(path string, status int, body string) *fakeCatalog {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[path] = fakeRoute{body: body, status: status}
	return f
}

// json registra una respuesta 200 con el cuerpo indicado.
func (f *fakeCatalog) json(path, body string) *fakeCatalog {
	return f.route(path, http.StatusOK, body)
}

// fixture registra una respuesta 200 con el contenido de un fixture de testdata.
func (f *fakeCatalog) fixture(t *testing.T, path, dir, file string) *fakeCatalog {
	t.Helper()
	return f.route(path, http.StatusOK, string(readFixture(t, dir, file)))
}

// status registra una respuesta con un estado HTTP concreto.
func (f *fakeCatalog) status(path string, code int) *fakeCatalog {
	return f.route(path, code, `{"error":"fallo simulado"}`)
}

// url es la URL base del servidor de pruebas.
func (f *fakeCatalog) url() string { return f.server.URL }

// hitPaths devuelve las rutas consultadas, en orden.
func (f *fakeCatalog) hitPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.hits))
	copy(out, f.hits)
	return out
}

// requireHits comprueba exactamente qué rutas se consultaron.
func (f *fakeCatalog) requireHits(t *testing.T, want ...string) {
	t.Helper()
	got := f.hitPaths()
	if len(got) != len(want) {
		t.Fatalf("peticiones = %v; quiero %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("peticiones = %v; quiero %v", got, want)
		}
	}
}

// ===========================================================================
// Paper (Fill API v3)
// ===========================================================================

// TestPaperBasePorDefectoEsFillV3 fija el endpoint oficial: la API v2 está
// retirada (devuelve 404) y no debe volver a usarse.
func TestPaperBasePorDefectoEsFillV3(t *testing.T) {
	p := &paperProvider{engine: Paper}
	if got := p.Base(); got != "https://fill.papermc.io/v3/projects/paper" {
		t.Fatalf("base de Paper = %q", got)
	}
	if strings.Contains(p.Base(), "api.papermc.io/v2") {
		t.Fatal("Paper no debe usar la API v2 (retirada)")
	}
	// La base inyectada manda (pruebas con httptest).
	p.base = "http://127.0.0.1:1234"
	if p.Base() != "http://127.0.0.1:1234" {
		t.Fatalf("base inyectada ignorada: %q", p.Base())
	}
}

// TestPaperListVersionsFillV3 comprueba el parseo del catálogo real (objetos de
// versión, no cadenas), el orden ascendente interno, el canal y la estabilidad.
func TestPaperListVersionsFillV3(t *testing.T) {
	f := newFakeCatalog(t)
	f.fixture(t, "/versions", "paper_fill_v3", "versions.json")

	client := staticTestClient()
	var records []LogRecord
	client.Log = func(rec LogRecord) { records = append(records, rec) }
	p := &paperProvider{engine: Paper, client: client, base: f.url()}

	list, err := p.ListVersions(context.Background())
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(list) != 50 {
		t.Fatalf("versiones = %d; el fixture tiene 50 entradas", len(list))
	}
	// El fixture (respuesta real) viene de la más nueva a la más antigua; el
	// contrato interno es ascendente para que pickVersion sea correcto.
	if list[0].Version != "1.7.10" {
		t.Fatalf("primera versión = %q; quiero la más antigua (1.7.10)", list[0].Version)
	}
	if last := list[len(list)-1]; last.Version != "26.3" || !last.Stable || last.Channel != ChannelRelease {
		t.Fatalf("última versión = %+v; quiero 26.3 release estable", last)
	}
	// La ventana de soporte del proveedor se expone como detalle, pero no decide
	// la estabilidad (1.21.11 es UNSUPPORTED y sigue siendo una release).
	byID := map[string]Version{}
	for _, v := range list {
		byID[v.Version] = v
	}
	if got := byID["26.3"].Detail; got != "SUPPORTED" {
		t.Fatalf("detail de 26.3 = %q; quiero SUPPORTED", got)
	}
	if got := byID["1.21.11"]; !got.Stable || got.Channel != ChannelRelease || !strings.HasPrefix(got.Detail, "UNSUPPORTED") {
		t.Fatalf("1.21.11 = %+v; quiero release estable no soportada", got)
	}
	// Pre-releases: canal beta y no estables (el catálogo público las filtra).
	preReleases := []string{"26.3-rc-3", "26.2-rc-2", "1.21.11-rc1", "1.21.11-pre3", "1.13-pre7"}
	for _, id := range preReleases {
		v, ok := byID[id]
		if !ok {
			t.Fatalf("falta la pre-release %q en el catálogo", id)
		}
		if v.Stable || v.Channel != ChannelBeta {
			t.Fatalf("%s = %+v; quiero canal beta y no estable", id, v)
		}
	}
	// Limitación conocida: /versions no expone fecha de publicación.
	for _, v := range list {
		if !v.ReleasedAt.IsZero() {
			t.Fatalf("%s trae fecha %v y Fill v3 no la publica en /versions", v.Version, v.ReleasedAt)
		}
	}
	// Diagnóstico etiquetado (RequestOpts).
	if len(records) != 1 || records[0].Provider != "paper" || records[0].Operation != "list_versions" {
		t.Fatalf("diagnóstico = %+v; quiero paper/list_versions", records)
	}
	f.requireHits(t, "/versions")
}

// TestPaperResolveVersionExplicitaConPinExacto resuelve una versión concreta
// contra las respuestas reales y exige el pin exacto del build.
func TestPaperResolveVersionExplicitaConPinExacto(t *testing.T) {
	f := newFakeCatalog(t)
	f.fixture(t, "/versions", "paper_fill_v3", "versions.json")
	f.fixture(t, "/versions/26.3/builds", "paper_fill_v3", "builds_26.3.json")

	p := &paperProvider{engine: Paper, client: staticTestClient(), base: f.url()}
	res, err := p.Resolve(context.Background(), "26.3")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Version != "26.3" {
		t.Fatalf("versión resuelta = %q", res.Version)
	}
	if got := res.Pins[Paper.DepVar]; got != "190" {
		t.Fatalf("pin %s = %q; el build estable más reciente del fixture es 190", Paper.DepVar, got)
	}
	if res.Detail != "build 190 (STABLE)" {
		t.Fatalf("detalle = %q", res.Detail)
	}
	f.requireHits(t, "/versions", "/versions/26.3/builds")
}

// TestPaperResolveUltimaEstable resuelve sin versión explícita y exige pin.
func TestPaperResolveUltimaEstable(t *testing.T) {
	f := newFakeCatalog(t)
	f.fixture(t, "/versions", "paper_fill_v3", "versions.json")
	f.fixture(t, "/versions/26.3/builds", "paper_fill_v3", "builds_26.3.json")

	p := &paperProvider{engine: Paper, client: staticTestClient(), base: f.url()}
	res, err := p.Resolve(context.Background(), "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Version != "26.3" || res.Pins[Paper.DepVar] != "190" {
		t.Fatalf("resolución = %+v; quiero 26.3 con PAPER_BUILD=190", res)
	}
}

// TestPaperResolveVersionInexistenteEsNotFound: una versión que no existe no
// puede devolver un resultado parcial (ni pin, ni versión).
func TestPaperResolveVersionInexistenteEsNotFound(t *testing.T) {
	f := newFakeCatalog(t)
	f.fixture(t, "/versions", "paper_fill_v3", "versions.json")

	p := &paperProvider{engine: Paper, client: staticTestClient(), base: f.url()}
	res, err := p.Resolve(context.Background(), "9.9.9")
	if err == nil {
		t.Fatalf("resolver 9.9.9 debía fallar, res = %+v", res)
	}
	if code := CodeOf(err); code != CodeNotFound {
		t.Fatalf("código = %q; quiero %q (%v)", code, CodeNotFound, err)
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("errors.Is(ErrNotFound) = false: %v", err)
	}
	if res.Version != "" || len(res.Pins) != 0 {
		t.Fatalf("resolución parcial prohibida: %+v", res)
	}
	// No se debe intentar descargar builds de una versión inexistente.
	f.requireHits(t, "/versions")
}

// TestPaperBuildsHTTP404PropagaError: un 404 de /builds se propaga tipado.
func TestPaperBuildsHTTP404PropagaError(t *testing.T) {
	f := newFakeCatalog(t)
	f.fixture(t, "/versions", "paper_fill_v3", "versions.json")
	// Cuerpo real de la API v2 retirada, como ejemplo de error del proveedor.
	f.route("/versions/1.21.4/builds", http.StatusNotFound, string(readFixture(t, "paper_v2", "versions.json")))

	p := &paperProvider{engine: Paper, client: staticTestClient(), base: f.url()}
	res, err := p.Resolve(context.Background(), "1.21.4")
	if err == nil {
		t.Fatalf("un 404 en /builds no puede resolver: %+v", res)
	}
	if code := CodeOf(err); code != CodeNotFound {
		t.Fatalf("código = %q; quiero %q (%v)", code, CodeNotFound, err)
	}
	if res.Version != "" || len(res.Pins) != 0 {
		t.Fatalf("resolución parcial prohibida: %+v", res)
	}
}

// TestPaperBuildsHTTP500PropagaError: un 5xx se propaga como upstream sin
// disponible (no se convierte en una resolución sin pin).
func TestPaperBuildsHTTP500PropagaError(t *testing.T) {
	f := newFakeCatalog(t)
	f.fixture(t, "/versions", "paper_fill_v3", "versions.json")
	f.status("/versions/26.3/builds", http.StatusInternalServerError)

	p := &paperProvider{engine: Paper, client: staticTestClient(), base: f.url()}
	res, err := p.Resolve(context.Background(), "26.3")
	if err == nil {
		t.Fatalf("un 500 en /builds no puede resolver: %+v", res)
	}
	if code := CodeOf(err); code != CodeUpstreamUnavailable {
		t.Fatalf("código = %q; quiero %q (%v)", code, CodeUpstreamUnavailable, err)
	}
	if res.Version != "" || len(res.Pins) != 0 {
		t.Fatalf("resolución parcial prohibida: %+v", res)
	}
}

// TestPaperEsquemaDistintoEsSchemaMismatch: la API v2 (u otra forma) no se
// acepta nunca en silencio.
func TestPaperEsquemaDistintoEsSchemaMismatch(t *testing.T) {
	t.Run("catalogo v2 con cadena", func(t *testing.T) {
		f := newFakeCatalog(t)
		f.json("/versions", `{"versions":["1.21.4","1.21.5"]}`)
		p := &paperProvider{engine: Paper, client: staticTestClient(), base: f.url()}
		if _, err := p.ListVersions(context.Background()); CodeOf(err) != CodeSchemaMismatch {
			t.Fatalf("código = %q; quiero %q (%v)", CodeOf(err), CodeSchemaMismatch, err)
		}
	})
	t.Run("catalogo vacio", func(t *testing.T) {
		f := newFakeCatalog(t)
		f.json("/versions", `{"versions":[]}`)
		p := &paperProvider{engine: Paper, client: staticTestClient(), base: f.url()}
		if _, err := p.ListVersions(context.Background()); CodeOf(err) != CodeSchemaMismatch {
			t.Fatalf("código = %q; quiero %q (%v)", CodeOf(err), CodeSchemaMismatch, err)
		}
	})
	t.Run("entrada de version sin id", func(t *testing.T) {
		f := newFakeCatalog(t)
		f.json("/versions", `{"versions":[{"builds":[1,2]}]}`)
		p := &paperProvider{engine: Paper, client: staticTestClient(), base: f.url()}
		if _, err := p.ListVersions(context.Background()); CodeOf(err) != CodeSchemaMismatch {
			t.Fatalf("código = %q; quiero %q (%v)", CodeOf(err), CodeSchemaMismatch, err)
		}
	})
	t.Run("builds con la forma v2", func(t *testing.T) {
		f := newFakeCatalog(t)
		f.fixture(t, "/versions", "paper_fill_v3", "versions.json")
		f.json("/versions/26.3/builds", `{"builds":[{"build":157,"channel":"default"}]}`)
		p := &paperProvider{engine: Paper, client: staticTestClient(), base: f.url()}
		res, err := p.Resolve(context.Background(), "26.3")
		if CodeOf(err) != CodeSchemaMismatch {
			t.Fatalf("código = %q; quiero %q (%v)", CodeOf(err), CodeSchemaMismatch, err)
		}
		if res.Version != "" || len(res.Pins) != 0 {
			t.Fatalf("resolución parcial prohibida: %+v", res)
		}
	})
}

// TestPaperDescartaBuildSinArtefacto: se elige el build estable más reciente con
// descarga de servidor; los experimentales y los que no exponen artefacto se
// descartan (no basta con "el último de la lista").
func TestPaperDescartaBuildSinArtefacto(t *testing.T) {
	builds := `[
		{"id":201,"channel":"ALPHA","downloads":{"server:default":{"url":"https://fill-data.papermc.io/x-201.jar"}}},
		{"id":200,"channel":"STABLE","downloads":{}},
		{"id":199,"channel":"STABLE","downloads":{"server:default":{"name":"paper-1.21.4-199.jar","url":"https://fill-data.papermc.io/x-199.jar"}}},
		{"id":198,"channel":"BETA","downloads":{"server:default":{"url":"https://fill-data.papermc.io/x-198.jar"}}}
	]`
	f := newFakeCatalog(t)
	f.fixture(t, "/versions", "paper_fill_v3", "versions.json")
	f.json("/versions/1.21.4/builds", builds)

	p := &paperProvider{engine: Paper, client: staticTestClient(), base: f.url()}
	res, err := p.Resolve(context.Background(), "1.21.4")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := res.Pins[Paper.DepVar]; got != "199" {
		t.Fatalf("pin = %q; quiero 199 (el 201 es ALPHA y el 200 no tiene artefacto)", got)
	}
	if res.Detail != "build 199 (STABLE)" {
		t.Fatalf("detalle = %q", res.Detail)
	}
}

// TestPaperBuildsSinNingunArtefactoFalla: si ningún build estable es utilizable,
// la resolución falla (nunca un Resolved sin pin).
func TestPaperBuildsSinNingunArtefactoFalla(t *testing.T) {
	f := newFakeCatalog(t)
	f.fixture(t, "/versions", "paper_fill_v3", "versions.json")
	f.json("/versions/26.3/builds", `[
		{"id":190,"channel":"STABLE","downloads":{"server:default":{"name":"paper-26.3-190.jar"}}},
		{"id":189,"channel":"STABLE","downloads":{}}
	]`)

	p := &paperProvider{engine: Paper, client: staticTestClient(), base: f.url()}
	res, err := p.Resolve(context.Background(), "26.3")
	if err == nil {
		t.Fatalf("sin artefacto descargable no puede resolver: %+v", res)
	}
	if code := CodeOf(err); code != CodeNotFound {
		t.Fatalf("código = %q; quiero %q (%v)", code, CodeNotFound, err)
	}
	if res.Version != "" || len(res.Pins) != 0 {
		t.Fatalf("resolución parcial prohibida: %+v", res)
	}
}

// TestPaperListaDeBuildsVaciaFalla: una lista vacía es un error, nunca un éxito.
func TestPaperListaDeBuildsVaciaFalla(t *testing.T) {
	f := newFakeCatalog(t)
	f.fixture(t, "/versions", "paper_fill_v3", "versions.json")
	f.json("/versions/26.3/builds", `[]`)

	p := &paperProvider{engine: Paper, client: staticTestClient(), base: f.url()}
	res, err := p.Resolve(context.Background(), "26.3")
	if err == nil {
		t.Fatalf("una lista de builds vacía no puede resolver: %+v", res)
	}
	if code := CodeOf(err); code != CodeNotFound {
		t.Fatalf("código = %q; quiero %q (%v)", code, CodeNotFound, err)
	}
	if res.Version != "" || len(res.Pins) != 0 {
		t.Fatalf("resolución parcial prohibida: %+v", res)
	}
}

// TestPaperPickStableBuildEsDeterminista prueba la selección pura: tolera el
// canal en minúsculas, prefiere `server:default` y usa las demás claves
// `server:*` solo como respaldo.
func TestPaperPickStableBuildEsDeterminista(t *testing.T) {
	builds := []paperBuild{
		{ID: 10, Channel: "stable", Downloads: map[string]paperArtifact{
			"server:mojang": {URL: "https://fill-data.papermc.io/10.jar"},
		}},
		{ID: 11, Channel: "Stable", Downloads: map[string]paperArtifact{
			"server:default": {URL: "https://fill-data.papermc.io/11.jar"},
		}},
		{ID: 12, Channel: "EXPERIMENTAL", Downloads: map[string]paperArtifact{
			"server:default": {URL: "https://fill-data.papermc.io/12.jar"},
		}},
	}
	got, ok := pickStableBuild(builds)
	if !ok {
		t.Fatal("quiero un build estable de respaldo")
	}
	if got.ID != 11 {
		t.Fatalf("build = %d; quiero 11", got.ID)
	}
	if _, ok := pickStableBuild([]paperBuild{{ID: 13, Channel: "BETA"}}); ok {
		t.Fatal("sin builds estables no se puede elegir")
	}
	// La clave `server:default` manda sobre cualquier otra variante.
	preferred := map[string]paperArtifact{
		"server:other":   {URL: "https://fill-data.papermc.io/a.jar"},
		"server:default": {URL: "https://fill-data.papermc.io/b.jar"},
	}
	artifact, ok := serverArtifact(preferred)
	if !ok || artifact.URL != "https://fill-data.papermc.io/b.jar" {
		t.Fatalf("artefacto = %+v; quiero la clave server:default", artifact)
	}
}
