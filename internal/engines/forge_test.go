package engines

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// forgeTestProvider levanta un servidor con los DOS catálogos de Forge
// (promociones y índice Maven de builds) y devuelve el proveedor apuntando a él.
func forgeTestProvider(t *testing.T, promotions, maven []byte) (*forgeProvider, *pathRecorder) {
	t.Helper()
	rec := &pathRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r.URL.Path)
		switch r.URL.Path {
		case "/net/minecraftforge/forge/promotions_slim.json":
			_, _ = w.Write(promotions)
		case "/net/minecraftforge/forge/maven-metadata.json":
			_, _ = w.Write(maven)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &forgeProvider{engine: Forge, client: providersTestClient(), base: srv.URL + "/net/minecraftforge/forge"}, rec
}

// forgeRealFixtures carga las promociones y el índice Maven reales.
func forgeRealFixtures(t *testing.T) (promotions, maven []byte) {
	t.Helper()
	return readProviderFixture(t, "testdata/forge/promotions_slim.json"),
		readProviderFixture(t, "testdata/forge/maven-metadata.json")
}

func forgeFind(list []Version, mc string) (Version, bool) {
	for _, v := range list {
		if v.Version == mc {
			return v, true
		}
	}
	return Version{}, false
}

// TestForgeResolucionExactaConPromocionesReales comprueba AC-03: el pin es el
// valor exacto "<mc>-<build>" de un artefacto que existe en el índice Maven de
// builds descargables, y la promoción `recommended` tiene prioridad sobre
// `latest` (con caída explícita a `latest` si la recomendada no es descargable).
func TestForgeResolucionExactaConPromocionesReales(t *testing.T) {
	promotions, maven := forgeRealFixtures(t)
	p, rec := forgeTestProvider(t, promotions, maven)
	ctx := context.Background()

	cases := []struct {
		name, requested, wantMC, wantPin, wantChannel string
	}{
		// 1.21.11: hay `recommended` (61.2.0) y es descargable ⇒ gana aunque
		// `latest` (61.2.3) sea numéricamente más nuevo.
		{"recommended tiene prioridad", "1.21.11", "1.21.11", "1.21.11-61.2.0", forgeChannelRecommended},
		// 1.21.10: la recomendada (60.1.0) NO está en el índice recortado ⇒ cae a `latest`.
		{"cae a latest si la recomendada no es descargable", "1.21.10", "1.21.10", "1.21.10-60.1.17", forgeChannelLatest},
		// 26.3: solo hay `latest` y es descargable.
		{"solo latest", "26.3", "26.3", "26.3-66.0.11", forgeChannelLatest},
		// Sin versión pedida: la más reciente de las promociones (numérica, no alfabética).
		{"última disponible", "", "26.3", "26.3-66.0.11", forgeChannelLatest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := p.Resolve(ctx, tc.requested)
			if err != nil {
				t.Fatalf("Resolve(%q): %v", tc.requested, err)
			}
			if res.Version != tc.wantMC {
				t.Fatalf("versión de Minecraft = %q; quiero %q", res.Version, tc.wantMC)
			}
			if got := res.Pins[Forge.DepVar]; got != tc.wantPin {
				t.Fatalf("FORGE_VERSION = %q; quiero %q", got, tc.wantPin)
			}
			if !strings.Contains(res.Detail, tc.wantChannel) {
				t.Fatalf("el detalle %q debería indicar el canal %q", res.Detail, tc.wantChannel)
			}
		})
	}

	// Un pin completo ya pedido se acepta tal cual.
	res, err := p.Resolve(ctx, "26.3-66.0.11")
	if err != nil {
		t.Fatalf("Resolve(pin exacto): %v", err)
	}
	if res.Version != "26.3" || res.Pins[Forge.DepVar] != "26.3-66.0.11" {
		t.Fatalf("pin exacto mal resuelto: %+v", res)
	}

	// Ambos catálogos se piden por sus rutas oficiales.
	for _, path := range rec.list() {
		if path != "/net/minecraftforge/forge/promotions_slim.json" && path != "/net/minecraftforge/forge/maven-metadata.json" {
			t.Fatalf("endpoint inesperado: %s", path)
		}
	}
}

// TestForgeListVersionsVieneDeLasPromociones comprueba que el catálogo NO queda
// vacío y que cada entrada es una versión de Minecraft con su build promocionado.
func TestForgeListVersionsVieneDeLasPromociones(t *testing.T) {
	promotions, maven := forgeRealFixtures(t)
	p, _ := forgeTestProvider(t, promotions, maven)

	list, err := p.ListVersions(context.Background())
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(list) < 40 {
		t.Fatalf("catálogo sospechosamente corto: %d entradas (las promociones reales traen 117 claves)", len(list))
	}
	if list[0].Version != "26.3" {
		t.Fatalf("la primera entrada es %q; quiero 26.3 (orden por versión, no alfabético)", list[0].Version)
	}
	for _, v := range list {
		if v.Build == "" {
			t.Fatalf("entrada sin build: %+v", v)
		}
		if !v.Stable || v.Channel != ChannelRelease {
			t.Fatalf("las versiones de Minecraft promocionadas son releases: %+v", v)
		}
	}
	v, ok := forgeFind(list, "1.21.1")
	if !ok {
		t.Fatal("falta 1.21.1 en el catálogo")
	}
	if v.Build != "52.1.0" {
		t.Fatalf("1.21.1 debe mostrar la promoción recomendada (52.1.0), tengo %q", v.Build)
	}
	v263, ok := forgeFind(list, "26.3")
	if !ok || v263.Build != "66.0.11" {
		t.Fatalf("26.3 debe mostrar su última promoción (66.0.11): %+v (ok=%v)", v263, ok)
	}
}

// TestForgeIndiceMavenVacioNoDejaCatalogoVacio es el caso API-06: un documento
// válido con otra estructura NO puede dejar el catálogo vacío en silencio.
func TestForgeIndiceMavenVacioNoDejaCatalogoVacio(t *testing.T) {
	promotions, _ := forgeRealFixtures(t)
	cases := []struct {
		name string
		body string
	}{
		{"objeto ajeno", `{"foo":1}`},
		{"catálogo vacío", `{}`},
		{"sin builds", `{"26.3":[]}`},
		{"esquema versioning (Maven genérico)", `{"versioning":{"versions":["26.3-66.0.11"]}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := forgeTestProvider(t, promotions, []byte(tc.body))
			res, err := p.Resolve(context.Background(), "26.3")
			if err == nil {
				t.Fatalf("quiero schema_mismatch, tengo una resolución: %+v", res)
			}
			if res.Version != "" || len(res.Pins) != 0 {
				t.Fatalf("resolución parcial prohibida: %+v", res)
			}
			if got := CodeOf(err); got != CodeSchemaMismatch {
				t.Fatalf("código = %q; quiero %q (error: %v)", got, CodeSchemaMismatch, err)
			}
		})
	}
}

// TestForgePromocionesSchemaDistintoFalla comprueba que `promotions_slim.json` se
// valida como el mapa de promociones que es.
func TestForgePromocionesSchemaDistintoFalla(t *testing.T) {
	_, maven := forgeRealFixtures(t)
	p, _ := forgeTestProvider(t, []byte(`{"foo":1}`), maven)

	if _, err := p.ListVersions(context.Background()); err == nil {
		t.Fatal("quiero error para promociones sin el objeto `promos`")
	} else if got := CodeOf(err); got != CodeSchemaMismatch {
		t.Fatalf("código = %q; quiero %q (error: %v)", got, CodeSchemaMismatch, err)
	}

	p, _ = forgeTestProvider(t, []byte(`{"homepage":"https://x","promos":{"a":"1","b":"2"}}`), maven)
	if _, err := p.ListVersions(context.Background()); err == nil {
		t.Fatal("quiero error para claves que no son \"<mc>-recommended|latest\"")
	} else if got := CodeOf(err); got != CodeSchemaMismatch {
		t.Fatalf("código = %q; quiero %q (error: %v)", got, CodeSchemaMismatch, err)
	}

	p, _ = forgeTestProvider(t, []byte(`{"promos":{"1.21.1-latest":123}}`), maven)
	if _, err := p.ListVersions(context.Background()); err == nil {
		t.Fatal("quiero error para valores que no son cadenas")
	} else if got := CodeOf(err); got != CodeSchemaMismatch {
		t.Fatalf("código = %q; quiero %q (error: %v)", got, CodeSchemaMismatch, err)
	}
}

// TestForgePromocionesVaciasFalla comprueba que unas promociones vacías dan un
// error legible (nunca un catálogo vacío silencioso).
func TestForgePromocionesVaciasFalla(t *testing.T) {
	_, maven := forgeRealFixtures(t)
	for _, body := range []string{
		`{"homepage":"https://files.minecraftforge.net","promos":{}}`,
		`{"promos":null}`,
		`{"homepage":"https://files.minecraftforge.net"}`,
		`{}`,
	} {
		p, _ := forgeTestProvider(t, []byte(body), maven)
		list, err := p.ListVersions(context.Background())
		if err == nil {
			t.Fatalf("quiero error legible con %s (lista: %+v)", body, list)
		}
		if !strings.Contains(err.Error(), "promotions_slim.json") {
			t.Fatalf("el error debería identificar el catálogo de promociones: %v", err)
		}
		if got := CodeOf(err); got != CodeSchemaMismatch && got != CodeInvalidResponse {
			t.Fatalf("código = %q; quiero schema_mismatch o invalid_response (error: %v)", got, err)
		}
	}
}

// TestForgeBuildNoDescargableFalla comprueba QC-04: una promoción cuyo build no
// existe en el índice Maven no se ofrece como resolución válida.
func TestForgeBuildNoDescargableFalla(t *testing.T) {
	promotions, _ := forgeRealFixtures(t)
	p, _ := forgeTestProvider(t, promotions, []byte(`{"26.3":["26.3-66.0.0"]}`))

	res, err := p.Resolve(context.Background(), "26.3")
	if err == nil {
		t.Fatalf("quiero error: 26.3-66.0.11 no es descargable (res=%+v)", res)
	}
	if res.Version != "" || len(res.Pins) != 0 {
		t.Fatalf("resolución parcial prohibida: %+v", res)
	}
	if got := CodeOf(err); got != CodeNotFound {
		t.Fatalf("código = %q; quiero %q (error: %v)", got, CodeNotFound, err)
	}
	if !strings.Contains(err.Error(), "26.3-66.0.11") {
		t.Fatalf("el error debería nombrar el build descartado: %v", err)
	}
}

// TestForgeVersionSinPromocionesFalla comprueba que pedir una versión de Minecraft
// que Forge no promociona falla de forma tipada.
func TestForgeVersionSinPromocionesFalla(t *testing.T) {
	promotions, maven := forgeRealFixtures(t)
	p, _ := forgeTestProvider(t, promotions, maven)

	res, err := p.Resolve(context.Background(), "9.9.9")
	if err == nil {
		t.Fatalf("quiero error (res=%+v)", res)
	}
	if got := CodeOf(err); got != CodeNotFound {
		t.Fatalf("código = %q; quiero %q (error: %v)", got, CodeNotFound, err)
	}
	if res.Version != "" || len(res.Pins) != 0 {
		t.Fatalf("resolución parcial prohibida: %+v", res)
	}
}
