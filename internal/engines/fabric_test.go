package engines

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Ayudas comunes de las pruebas de proveedores (Fabric/Forge/NeoForge)
// ---------------------------------------------------------------------------

// providersTestClient construye el transporte de pruebas de los proveedores: sin
// reintentos y con tiempos cortos. Los fixtures sustituyen a la red: estas
// pruebas NO llaman a internet.
func providersTestClient() *Client {
	return &Client{
		HTTP:        &http.Client{Timeout: 2 * time.Second},
		UserAgent:   "badomens-test",
		MaxAttempts: 1,
	}
}

// readProviderFixture lee un fixture de testdata.
func readProviderFixture(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatalf("no se pudo leer el fixture %s: %v", path, err)
	}
	return raw
}

// pathRecorder registra, con seguridad para uso concurrente, las rutas pedidas al
// servidor de pruebas: es la forma de verificar QUÉ endpoint pidió el adaptador.
type pathRecorder struct {
	mu    sync.Mutex
	paths []string
}

func (r *pathRecorder) add(p string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paths = append(r.paths, p)
}

func (r *pathRecorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.paths...)
}

// ---------------------------------------------------------------------------
// Fabric
// ---------------------------------------------------------------------------

// fabricTestProvider levanta un servidor que sirve los catálogos de Fabric Meta
// bajo /v2 y devuelve el proveedor apuntando a él.
func fabricTestProvider(t *testing.T, game []byte, loaders func(mc string) ([]byte, int)) (*fabricProvider, *pathRecorder) {
	t.Helper()
	rec := &pathRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r.URL.Path)
		switch {
		case r.URL.Path == "/v2/versions/game":
			_, _ = w.Write(game)
		default:
			const prefix = "/v2/versions/loader/"
			if mc, ok := cutPrefix(r.URL.Path, prefix); ok {
				body, status := loaders(mc)
				if status != http.StatusOK {
					http.Error(w, "fallo simulado del proveedor", status)
					return
				}
				_, _ = w.Write(body)
				return
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &fabricProvider{engine: Fabric, client: providersTestClient(), base: srv.URL + "/v2"}, rec
}

// cutPrefix es strings.CutPrefix con soporte de "no coincide".
func cutPrefix(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return "", false
}

// TestFabricLoaderPorGameVersion verifica el flujo obligatorio de API-04: la
// versión de Minecraft se elige del catálogo de juego y el loader se consulta en
// el endpoint ESPECÍFICO /v2/versions/loader/{game_version}, nunca en el catálogo
// general /v2/versions/loader.
func TestFabricLoaderPorGameVersion(t *testing.T) {
	game := readProviderFixture(t, "testdata/fabric/versions_game.json")
	loaders := readProviderFixture(t, "testdata/fabric/loader_26.3.json")

	p, rec := fabricTestProvider(t, game, func(string) ([]byte, int) { return loaders, http.StatusOK })

	res, err := p.Resolve(context.Background(), "26.3")
	if err != nil {
		t.Fatalf("Resolve(26.3): %v", err)
	}
	if res.Version != "26.3" {
		t.Fatalf("versión de Minecraft = %q; quiero 26.3", res.Version)
	}
	if got := res.Pins[Fabric.DepVar]; got != "0.19.5" {
		t.Fatalf("FABRIC_LOADER_VERSION = %q; quiero 0.19.5 (el único loader estable del fixture)", got)
	}
	want := []string{"/v2/versions/game", "/v2/versions/loader/26.3"}
	if got := rec.list(); !reflect.DeepEqual(got, want) {
		t.Fatalf("peticiones = %v; quiero %v (el loader NUNCA se pide al catálogo general)", got, want)
	}
}

// TestFabricLoaderIndependienteDelOrden comprueba que el loader elegido no
// depende del orden de la respuesta: Fabric Meta publica newest-first, pero el
// adaptador compara versiones y aplica la política de estabilidad.
func TestFabricLoaderIndependienteDelOrden(t *testing.T) {
	game := readProviderFixture(t, "testdata/fabric/versions_game.json")
	stable := func(v string) fabricLoaderEntry {
		var e fabricLoaderEntry
		e.Loader.Version, e.Loader.Stable = v, true
		return e
	}
	unstable := func(v string) fabricLoaderEntry {
		var e fabricLoaderEntry
		e.Loader.Version, e.Loader.Stable = v, false
		return e
	}
	orders := map[string][]fabricLoaderEntry{
		"newest-first": {stable("0.19.5"), unstable("0.19.4"), unstable("0.19.3")},
		"invertido":    {unstable("0.19.3"), unstable("0.19.4"), stable("0.19.5")},
		// Un loader INESTABLE numéricamente más nuevo no puede ganar: la política
		// filtra antes de comparar.
		"mezclado": {unstable("0.20.0"), unstable("0.19.3"), stable("0.19.5"), unstable("0.19.4")},
	}
	for name, entries := range orders {
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(entries)
			if err != nil {
				t.Fatal(err)
			}
			p, _ := fabricTestProvider(t, game, func(string) ([]byte, int) { return body, http.StatusOK })
			res, err := p.Resolve(context.Background(), "26.3")
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got := res.Pins[Fabric.DepVar]; got != "0.19.5" {
				t.Fatalf("loader = %q; quiero 0.19.5 en el orden %s", got, name)
			}
		})
	}
}

// TestFabricLatestStableNoDependeDelOrden es la prueba de API-05: Fabric Meta
// entrega las versiones más nuevas primero y ninguna posición de la lista puede
// usarse como "la más reciente".
func TestFabricLatestStableNoDependeDelOrden(t *testing.T) {
	var games []fabricGame
	if err := json.Unmarshal(readProviderFixture(t, "testdata/fabric/versions_game.json"), &games); err != nil {
		t.Fatalf("fixture de juego: %v", err)
	}
	if len(games) == 0 {
		t.Fatal("el fixture de juego está vacío")
	}
	// Se comprueba que el fixture es newest-first: la primera entrada es un
	// snapshot MÁS NUEVO que la primera estable, que ya aparece en la lista.
	if games[0].Stable || compareProviderVersions(games[0].Version, "26.3") <= 0 {
		t.Fatalf("el fixture se esperaba newest-first, con un snapshot más nuevo que 26.3: %+v", games[0])
	}
	hasStable := false
	for _, g := range games {
		if g.Version == "26.3" && g.Stable {
			hasStable = true
		}
	}
	if !hasStable {
		t.Fatal("el fixture debe incluir la versión estable 26.3")
	}
	reversed := make([]fabricGame, len(games))
	for i := range games {
		reversed[i] = games[len(games)-1-i]
	}
	mixed := make([]fabricGame, 0, len(games))
	for i := 0; i < len(games); i += 2 {
		mixed = append(mixed, games[i])
	}
	for i := 1; i < len(games); i += 2 {
		mixed = append(mixed, games[i])
	}
	orders := map[string][]fabricGame{
		"newest-first": games,
		"invertido":    reversed,
		"mezclado":     mixed,
	}
	for name, order := range orders {
		got, ok := fabricLatestStable(order)
		if !ok || got != "26.3" {
			t.Fatalf("orden %s: última estable = %q (ok=%v); quiero 26.3", name, got, ok)
		}
	}
}

// TestFabricResolveSinLoaderFalla comprueba T-02/AC-06: sin loader confirmado no
// hay resolución, y NUNCA se devuelve un Resolved parcial.
func TestFabricResolveSinLoaderFalla(t *testing.T) {
	game := readProviderFixture(t, "testdata/fabric/versions_game.json")
	cases := []struct {
		name     string
		body     []byte
		status   int
		wantCode string
	}{
		{"loaders vacíos", []byte(`[]`), http.StatusOK, CodeInvalidResponse},
		{"solo loaders inestables", []byte(`[{"loader":{"version":"0.19.4","stable":false}}]`), http.StatusOK, CodeInvalidResponse},
		{"error 500 del proveedor", nil, http.StatusInternalServerError, CodeUpstreamUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, rec := fabricTestProvider(t, game, func(string) ([]byte, int) { return tc.body, tc.status })
			res, err := p.Resolve(context.Background(), "26.3")
			if err == nil {
				t.Fatalf("quiero error: sin loader confirmado la resolución no es válida (res=%+v)", res)
			}
			if res.Version != "" || len(res.Pins) != 0 {
				t.Fatalf("resolución parcial prohibida: %+v", res)
			}
			if got := CodeOf(err); got != tc.wantCode {
				t.Fatalf("código = %q; quiero %q (error: %v)", got, tc.wantCode, err)
			}
			if len(rec.list()) == 0 {
				t.Fatal("se esperaba al menos una consulta al proveedor")
			}
		})
	}
}

// TestFabricGameVersionInexistenteFalla verifica que una versión de Minecraft que
// no existe falla ANTES de consultar loaders (no se resuelve "algo parecido").
func TestFabricGameVersionInexistenteFalla(t *testing.T) {
	game := readProviderFixture(t, "testdata/fabric/versions_game.json")
	p, rec := fabricTestProvider(t, game, func(mc string) ([]byte, int) {
		t.Errorf("no se debía consultar el loader de %q: la versión de Minecraft no existe", mc)
		return []byte(`[]`), http.StatusOK
	})
	res, err := p.Resolve(context.Background(), "9.9.9")
	if err == nil {
		t.Fatalf("quiero error para una versión inexistente (res=%+v)", res)
	}
	if got := CodeOf(err); got != CodeNotFound {
		t.Fatalf("código = %q; quiero %q (error: %v)", got, CodeNotFound, err)
	}
	if res.Version != "" || len(res.Pins) != 0 {
		t.Fatalf("resolución parcial prohibida: %+v", res)
	}
	if got := rec.list(); len(got) != 1 || got[0] != "/v2/versions/game" {
		t.Fatalf("peticiones = %v; solo se esperaba el catálogo de juego", got)
	}
}

// TestFabricCatalogoDeJuegoFalla comprueba que un fallo del catálogo de juego se
// propaga tipado y sin resolución parcial.
func TestFabricCatalogoDeJuegoFalla(t *testing.T) {
	cases := []struct {
		name     string
		body     []byte
		wantCode string
	}{
		{"catálogo vacío", []byte(`[]`), CodeInvalidResponse},
		{"respuesta no JSON", []byte(`<html>error</html>`), CodeInvalidResponse},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := fabricTestProvider(t, tc.body, func(string) ([]byte, int) { return []byte(`[]`), http.StatusOK })
			res, err := p.Resolve(context.Background(), "")
			if err == nil {
				t.Fatalf("quiero error (res=%+v)", res)
			}
			if res.Version != "" || len(res.Pins) != 0 {
				t.Fatalf("resolución parcial prohibida: %+v", res)
			}
			if got := CodeOf(err); got != tc.wantCode {
				t.Fatalf("código = %q; quiero %q (error: %v)", got, tc.wantCode, err)
			}
		})
	}
}

// TestCompareProviderVersions fija el contrato del comparador local: numérico por
// componente y con los sufijos por debajo de la versión base.
func TestCompareProviderVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"26.3", "26.2", 1},
		{"1.21.10", "1.21.9", 1},
		{"20.2.29-beta", "20.2.9-beta", 1}, // alfabéticamente sería al revés
		{"26.1.0.10", "26.1.0.3", 1},
		{"21.1.10-beta", "21.1.9-beta", 1},
		{"26.3", "26.3", 0},
		{"26.3-snapshot-4", "26.3", -1}, // sufijo con el MISMO prefijo numérico
		{"26.4-snapshot-4", "26.3", 1},  // snapshot de una versión base más nueva
		{"26.3.0.77-beta", "26.3.0.77", -1},
		{"1.21", "1.21.1", -1},
	}
	for _, tc := range cases {
		got := compareProviderVersions(tc.a, tc.b)
		if (got < 0) != (tc.want < 0) || (got > 0) != (tc.want > 0) {
			t.Fatalf("compare(%q, %q) = %d; quiero signo %d", tc.a, tc.b, got, tc.want)
		}
		if rev := compareProviderVersions(tc.b, tc.a); (rev < 0) != (tc.want > 0) || (rev > 0) != (tc.want < 0) {
			t.Fatalf("el comparador no es simétrico con %q/%q: %d", tc.a, tc.b, rev)
		}
	}
}
