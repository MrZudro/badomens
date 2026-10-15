package engines

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// switchBody permite cambiar el XML servido entre consultas (para probar la
// detección de caídas anómalas) con seguridad para uso concurrente.
type switchBody struct {
	mu   sync.Mutex
	body []byte
}

func (s *switchBody) set(raw []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.body = raw
}

func (s *switchBody) get() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body
}

// staticBody sirve siempre el mismo XML.
func staticBody(raw []byte) func() []byte { return func() []byte { return raw } }

// neoforgeTestProvider levanta un servidor que sirve el XML en la ruta OFICIAL
// del Maven de NeoForge (maven-metadata.xml; nunca .json).
func neoforgeTestProvider(t *testing.T, body func() []byte) (*neoforgeProvider, *pathRecorder) {
	t.Helper()
	rec := &pathRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r.URL.Path)
		if r.URL.Path != "/releases/net/neoforged/neoforge/maven-metadata.xml" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body())
	}))
	t.Cleanup(srv.Close)
	return &neoforgeProvider{
		engine: NeoForge,
		client: providersTestClient(),
		base:   srv.URL + "/releases/net/neoforged/neoforge",
	}, rec
}

// TestNeoForgeXmlEsquemaModernoResuelveConPin comprueba que se parsea el XML
// oficial (no el .json que da 404) y que el esquema moderno (MC 26.x ↔ 26.x.y.build)
// se resuelve con su pin exacto.
func TestNeoForgeXmlEsquemaModernoResuelveConPin(t *testing.T) {
	real := readProviderFixture(t, "testdata/neoforge/maven-metadata.xml")
	p, rec := neoforgeTestProvider(t, staticBody(real))
	ctx := context.Background()

	cases := []struct {
		requested, wantMC, wantPin string
	}{
		{"26.3", "26.3", "26.3.0.77"},
		// 26.1 tiene 14 alphas y un build estable: gana la estable.
		{"26.1", "26.1", "26.1.2.117"},
		{"", "26.3", "26.3.0.77"},
		{"26.3.0.77", "26.3", "26.3.0.77"},
	}
	for _, tc := range cases {
		res, err := p.Resolve(ctx, tc.requested)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", tc.requested, err)
		}
		if res.Version != tc.wantMC {
			t.Fatalf("Resolve(%q): versión de Minecraft = %q; quiero %q", tc.requested, res.Version, tc.wantMC)
		}
		if got := res.Pins[NeoForge.DepVar]; got != tc.wantPin {
			t.Fatalf("Resolve(%q): NEOFORGE_VERSION = %q; quiero %q", tc.requested, got, tc.wantPin)
		}
	}

	for _, path := range rec.list() {
		if !strings.HasSuffix(path, "/maven-metadata.xml") {
			t.Fatalf("endpoint inesperado (el oficial es XML): %s", path)
		}
	}
}

// TestNeoForgeXmlEsquemaAntiguoResuelveConPin comprueba el otro esquema
// coexistente: antiguo "21.1.x" ↔ Minecraft "1.21.1" y "20.2.x" ↔ "1.20.2".
func TestNeoForgeXmlEsquemaAntiguoResuelveConPin(t *testing.T) {
	legacy := readProviderFixture(t, "testdata/neoforge/maven-metadata_legacy.xml")
	p, _ := neoforgeTestProvider(t, staticBody(legacy))
	ctx := context.Background()

	res, err := p.Resolve(ctx, "1.21.1")
	if err != nil {
		t.Fatalf("Resolve(1.21.1): %v", err)
	}
	if res.Version != "1.21.1" {
		t.Fatalf("versión de Minecraft = %q; quiero 1.21.1", res.Version)
	}
	// El máximo es NUMÉRICO (21.1.72), no el máximo alfabético (21.1.9).
	if got := res.Pins[NeoForge.DepVar]; got != "21.1.72-beta" {
		t.Fatalf("NEOFORGE_VERSION = %q; quiero 21.1.72-beta (no 21.1.9-beta, que es el máximo alfabético)", got)
	}

	real := readProviderFixture(t, "testdata/neoforge/maven-metadata.xml")
	p2, _ := neoforgeTestProvider(t, staticBody(real))
	res, err = p2.Resolve(ctx, "1.20.2")
	if err != nil {
		t.Fatalf("Resolve(1.20.2): %v", err)
	}
	if got := res.Pins[NeoForge.DepVar]; got != "20.2.29-beta" {
		t.Fatalf("NEOFORGE_VERSION = %q; quiero 20.2.29-beta (no 20.2.9-beta)", got)
	}
	if !strings.Contains(res.Detail, string(ChannelBeta)) {
		t.Fatalf("el detalle debe advertir de que no hay build estable: %q", res.Detail)
	}
}

// TestNeoForgeXmlInvalidoFalla comprueba los errores tipados de esquema/vacío.
func TestNeoForgeXmlInvalidoFalla(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantCode string
	}{
		{
			"sin versioning",
			`<metadata><groupId>net.neoforged</groupId><artifactId>neoforge</artifactId></metadata>`,
			CodeSchemaMismatch,
		},
		{
			"versions vacío",
			`<metadata><versioning><versions></versions></versioning></metadata>`,
			CodeInvalidResponse,
		},
		{
			"versiones en blanco",
			`<metadata><versioning><versions><version>   </version></versions></versioning></metadata>`,
			CodeInvalidResponse,
		},
		{
			"ninguna versión asociable",
			`<metadata><versioning><versions><version>0.25w14craftmine.3-beta</version></versions></versioning></metadata>`,
			CodeInvalidResponse,
		},
		{
			// Un documento truncado lo clasifica el transporte como invalid_response
			// (no como esquema distinto); lo exigible aquí es que sea un error
			// TIPADO y que no se publique un catálogo vacío.
			"XML truncado",
			`<metadata><versioning><versions><version>26.3.0.77`,
			CodeInvalidResponse,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := neoforgeTestProvider(t, staticBody([]byte(tc.body)))
			list, err := p.ListVersions(context.Background())
			if err == nil {
				t.Fatalf("quiero error tipado (lista: %+v)", list)
			}
			if len(list) != 0 {
				t.Fatalf("no se debe publicar catálogo tras un fallo: %+v", list)
			}
			if got := CodeOf(err); got != tc.wantCode {
				t.Fatalf("código = %q; quiero %q (error: %v)", got, tc.wantCode, err)
			}
		})
	}
}

// TestNeoForgeCaidaAnomalaAdvierteYConserva es AC-04: la caída drástica del
// número de versiones (la regresión real de sep-2026 dejó 3) se marca con
// advertencia, se conserva el último catálogo validado y NO se sobrescribe.
func TestNeoForgeCaidaAnomalaAdvierteYConserva(t *testing.T) {
	real := readProviderFixture(t, "testdata/neoforge/maven-metadata.xml")
	publishedReal := bytes.Count(real, []byte("<version>"))
	if publishedReal < neoforgeMinBaseline {
		t.Fatalf("el fixture real debería traer muchas versiones; trae %d", publishedReal)
	}
	small := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<metadata>
  <groupId>net.neoforged</groupId>
  <artifactId>neoforge</artifactId>
  <versioning>
    <latest>26.3.0.77</latest>
    <release>26.3.0.77</release>
    <versions>
      <version>26.3.0.75</version>
      <version>26.3.0.76</version>
      <version>26.3.0.77</version>
    </versions>
  </versioning>
</metadata>`)

	body := &switchBody{body: real}
	p, _ := neoforgeTestProvider(t, body.get)
	ctx := context.Background()

	first, err := p.Catalog(ctx)
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	if first.Retained {
		t.Fatal("la primera consulta no puede retener nada")
	}
	for _, w := range first.Warnings {
		if w.Code == CodeCatalogDrop {
			t.Fatalf("no se esperaba una caída en la primera consulta: %+v", w)
		}
	}

	// Llega el catálogo anómalo.
	body.set(small)
	second, err := p.Catalog(ctx)
	if err != nil {
		t.Fatalf("Catalog (caída): %v", err)
	}
	if !second.Retained {
		t.Fatal("quiero que se conserve el último catálogo validado")
	}
	if len(second.Warnings) == 0 || second.Warnings[0].Code != CodeCatalogDrop {
		t.Fatalf("quiero una advertencia catalog_drop: %+v", second.Warnings)
	}
	w := second.Warnings[0]
	if w.Previous != publishedReal || w.Current != 3 {
		t.Fatalf("advertencia con datos incorrectos: previo=%d actual=%d (quiero %d y 3)", w.Previous, w.Current, publishedReal)
	}
	if !strings.Contains(w.Message, "conserva") {
		t.Fatalf("la advertencia debe ser legible y explicar la conservación: %q", w.Message)
	}
	if !reflect.DeepEqual(second.Versions, first.Versions) {
		t.Fatalf("se debe servir el catálogo previo:\n previo=%+v\n actual=%+v", first.Versions, second.Versions)
	}

	// La resolución sigue funcionando sobre el catálogo conservado, avisando.
	res, err := p.Resolve(ctx, "26.3")
	if err != nil {
		t.Fatalf("Resolve con catálogo conservado: %v", err)
	}
	if res.Pins[NeoForge.DepVar] != "26.3.0.77" {
		t.Fatalf("pin = %q", res.Pins[NeoForge.DepVar])
	}
	if !strings.Contains(res.Detail, "aviso:") {
		t.Fatalf("la resolución debe propagar el aviso: %q", res.Detail)
	}

	// El catálogo validado NO se sobrescribió: al volver el bueno no hay caída y
	// el previo sigue siendo el grande.
	body.set(real)
	third, err := p.Catalog(ctx)
	if err != nil {
		t.Fatalf("Catalog (recuperado): %v", err)
	}
	if third.Retained {
		t.Fatal("con el catálogo bueno no se debe retener nada")
	}
	if third.PreviousCount != publishedReal {
		t.Fatalf("el catálogo conservado se sobrescribió: PreviousCount = %d; quiero %d", third.PreviousCount, publishedReal)
	}
	if !reflect.DeepEqual(third.Versions, first.Versions) {
		t.Fatal("el catálogo recuperado debe coincidir con el validado")
	}
}

// TestNeoForgeCatalogNormalizadoDelFixtureReal fija el resultado del XML real:
// una entrada por versión de Minecraft, con la nomenclatura no asociable aparte.
func TestNeoForgeCatalogNormalizadoDelFixtureReal(t *testing.T) {
	real := readProviderFixture(t, "testdata/neoforge/maven-metadata.xml")
	p, _ := neoforgeTestProvider(t, staticBody(real))

	cat, err := p.Catalog(context.Background())
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	// Orden de publicación conservado (no alfabético ni de esquema mezclado).
	wantMC := []string{"26.1", "1.20.2", "26.3"}
	gotMC := make([]string, 0, len(cat.Versions))
	for _, v := range cat.Versions {
		gotMC = append(gotMC, v.Version)
	}
	if !reflect.DeepEqual(gotMC, wantMC) {
		t.Fatalf("versiones de Minecraft = %v; quiero %v (orden de publicación)", gotMC, wantMC)
	}
	pins := map[string]string{"26.1": "26.1.2.117", "1.20.2": "20.2.29-beta", "26.3": "26.3.0.77"}
	for _, v := range cat.Versions {
		if pins[v.Version] != v.Build {
			t.Fatalf("%s: pin = %q; quiero %q", v.Version, v.Build, pins[v.Version])
		}
	}
	// Las versiones con nomenclatura no reconocida no entran en el catálogo, pero
	// se cuentan y se advierten: su compatibilidad no se puede comprobar.
	if cat.Unmapped != 3 {
		t.Fatalf("no asociables = %d; quiero 3 (0.25w14craftmine.*)", cat.Unmapped)
	}
	if len(cat.Warnings) != 1 || cat.Warnings[0].Code != CodeCatalogUnmapped {
		t.Fatalf("quiero una advertencia catalog_unmapped: %+v", cat.Warnings)
	}
	if !strings.Contains(cat.Warnings[0].Message, "no se pueden asociar") {
		t.Fatalf("advertencia poco clara: %q", cat.Warnings[0].Message)
	}
	for _, v := range cat.Versions {
		if strings.Contains(v.Build, "craftmine") {
			t.Fatalf("una versión no asociable no puede estar en el catálogo: %+v", v)
		}
	}
	// Warnings() expone lo mismo para quien entra por la interfaz Provider.
	if len(p.Warnings()) != 1 {
		t.Fatalf("Warnings() = %+v", p.Warnings())
	}
}

// TestNeoForgeNoOrdenaAlfabeticamenteEntreEsquemas es la prueba explícita de que
// versiones de esquemas distintos NUNCA se ordenan alfabéticamente: el orden
// publicado se conserva y la elección del build es numérica dentro de cada
// versión de Minecraft.
func TestNeoForgeNoOrdenaAlfabeticamenteEntreEsquemas(t *testing.T) {
	// El documento publica primero el esquema MODERNO (26.1) y después el ANTIGUO
	// (21.1.x de Minecraft 1.21.1): un orden alfabético los invertiría ("1.21.1"
	// < "26.1"). En cada grupo el máximo alfabético NO es el máximo numérico.
	mixed := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<metadata>
  <groupId>net.neoforged</groupId>
  <artifactId>neoforge</artifactId>
  <versioning>
    <latest>26.1.0.10</latest>
    <release>26.1.0.10</release>
    <versions>
      <version>26.1.0.2</version>
      <version>26.1.0.10</version>
      <version>26.1.0.3</version>
      <version>21.1.2-beta</version>
      <version>21.1.20-beta</version>
      <version>21.1.9-beta</version>
      <version>21.1.72-beta</version>
    </versions>
  </versioning>
</metadata>`)
	p, _ := neoforgeTestProvider(t, staticBody(mixed))
	ctx := context.Background()

	list, err := p.ListVersions(ctx)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("quiero 2 versiones de Minecraft, tengo %d: %+v", len(list), list)
	}
	if list[0].Version != "26.1" || list[1].Version != "1.21.1" {
		t.Fatalf("se debe conservar el orden publicado (26.1 antes de 1.21.1), tengo %q, %q", list[0].Version, list[1].Version)
	}
	if list[0].Channel != ChannelRelease || !list[0].Stable {
		t.Fatalf("26.1 tiene builds estables: %+v", list[0])
	}
	if list[1].Channel != ChannelBeta || list[1].Stable {
		t.Fatalf("1.21.1 solo tiene builds beta: %+v", list[1])
	}

	res, err := p.Resolve(ctx, "1.21.1")
	if err != nil {
		t.Fatalf("Resolve(1.21.1): %v", err)
	}
	if got := res.Pins[NeoForge.DepVar]; got == "21.1.9-beta" {
		t.Fatal("se ordenó alfabéticamente: 21.1.9-beta no es el build más reciente")
	} else if got != "21.1.72-beta" {
		t.Fatalf("pin = %q; quiero 21.1.72-beta", got)
	}

	res, err = p.Resolve(ctx, "26.1")
	if err != nil {
		t.Fatalf("Resolve(26.1): %v", err)
	}
	if got := res.Pins[NeoForge.DepVar]; got == "26.1.0.3" {
		t.Fatal("se ordenó alfabéticamente: 26.1.0.3 no es el build más reciente")
	} else if got != "26.1.0.10" {
		t.Fatalf("pin = %q; quiero 26.1.0.10", got)
	}
}

// TestNeoForgeVersionInexistenteFalla comprueba que una versión de Minecraft sin
// builds asociados no se resuelve "a lo parecido".
func TestNeoForgeVersionInexistenteFalla(t *testing.T) {
	legacy := readProviderFixture(t, "testdata/neoforge/maven-metadata_legacy.xml")
	p, _ := neoforgeTestProvider(t, staticBody(legacy))

	res, err := p.Resolve(context.Background(), "1.20.4")
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
