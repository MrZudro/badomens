package marketplace

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/errx"
)

// TestEvaluateEsConservador fija la regla central: solo es Compatible cuando el
// proveedor declara loader Y versión de Minecraft y ambos coinciden. Cualquier
// dato ausente deja el veredicto en Unknown; nunca se colapsa a compatible.
func TestEvaluateEsConservador(t *testing.T) {
	paper := Environment{Engine: "paper", Loader: "paper", GameVersion: "1.21.1"}

	cases := []struct {
		name         string
		loaders      []string
		gameVersions []string
		env          Environment
		want         Compatibility
		wantIn       string
	}{
		{"compatible exacto", []string{"paper"}, []string{"1.21.1"}, paper, Compatible, "paper"},
		{"sin loader declarado", nil, []string{"1.21.1"}, paper, Unknown, "no declara loaders"},
		{"sin versiones declaradas", []string{"paper"}, nil, paper, Unknown, "no declara versiones"},
		{"sin entorno", []string{"paper"}, []string{"1.21.1"}, Environment{}, Unknown, "no se indicó"},
		{"sin versión de Minecraft", []string{"paper"}, []string{"1.21.1"},
			Environment{Engine: "paper"}, Unknown, "versión de Minecraft"},
		{"loader distinto", []string{"spigot"}, []string{"1.21.1"}, paper, Incompatible, "spigot"},
		{"versión distinta", []string{"paper"}, []string{"1.20.1"}, paper, Incompatible, "1.20.1"},
		{"mayúsculas del proveedor", []string{"PAPER"}, []string{"1.21.1"}, paper, Compatible, "paper"},
		{"spigot no equivale a paper", []string{"spigot", "bukkit"}, []string{"1.21.1"}, paper, Incompatible, "paper"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := Evaluate(tc.loaders, tc.gameVersions, tc.env)
			if got != tc.want {
				t.Errorf("Evaluate = %q (%s), quiero %q", got, reason, tc.want)
			}
			if tc.wantIn != "" && !strings.Contains(reason, tc.wantIn) {
				t.Errorf("motivo = %q, debería explicar %q", reason, tc.wantIn)
			}
		})
	}
}

// TestEvaluateMotivoAcotado: el motivo no crece sin límite con listas largas.
func TestEvaluateMotivoAcotado(t *testing.T) {
	many := make([]string, 40)
	for i := range many {
		many[i] = "1.0." + string(rune('a'+i%26))
	}
	_, reason := Evaluate([]string{"paper"}, many, Environment{Engine: "paper", Loader: "paper", GameVersion: "9.9.9"})
	if !strings.Contains(reason, "más)") {
		t.Errorf("el motivo debería acotar la lista: %q", reason)
	}
	if len(reason) > 300 {
		t.Errorf("motivo demasiado largo (%d caracteres)", len(reason))
	}
}

// TestKindAccepts: el tipo de proyecto debe encajar con el motor.
func TestKindAccepts(t *testing.T) {
	cases := []struct {
		kind   Kind
		engine string
		want   bool
	}{
		{KindPlugin, "paper", true},
		{KindMod, "paper", false},
		{KindMod, "fabric", true},
		{KindMod, "forge", true},
		{KindMod, "neoforge", true},
		{KindPlugin, "fabric", false},
		{KindModpack, "paper", false},
		{KindModpack, "fabric", false},
		{KindCovered(), "paper", false},
		{KindPlugin, "vanilla", false},
		{KindPlugin, "paper", true},
	}
	for _, tc := range cases {
		if got := tc.kind.Accepts(tc.engine); got != tc.want {
			t.Errorf("(%q).Accepts(%q) = %v, quiero %v", tc.kind, tc.engine, got, tc.want)
		}
	}
	if !KnownLoader("paper") || !KnownLoader("NeoForge") || KnownLoader("vanilla") || KnownLoader("") {
		t.Error("KnownLoader no reconoce el vocabulario de motores")
	}
}

// KindCovered devuelve un tipo no instalable, para el caso negativo.
func KindCovered() Kind { return KindResourcepack }

// TestLoaderNameCaeAlMotor: sin loader explícito se usa el motor.
func TestLoaderNameCaeAlMotor(t *testing.T) {
	if got := (Environment{Engine: "paper"}).LoaderName(); got != "paper" {
		t.Errorf("LoaderName = %q", got)
	}
	if got := (Environment{Engine: "paper", Loader: "Folia"}).LoaderName(); got != "Folia" {
		t.Errorf("LoaderName = %q, debe respetar el loader explícito", got)
	}
	if got := (Environment{}).LoaderName(); got != "" {
		t.Errorf("LoaderName = %q, quiero vacío", got)
	}
}

// TestFileHashes: se prefiere sha256 y, si no lo hay, sha512; sha1 nunca se usa
// como prueba de integridad, aunque se conserve para informarlo.
func TestFileHashes(t *testing.T) {
	f := File{Hashes: []Hash{
		{Algorithm: HashSHA1, Value: strings.Repeat("a", 40)},
		{Algorithm: HashSHA512, Value: strings.Repeat("b", 128)},
	}}
	got, ok := f.StrongestHash()
	if !ok || got.Algorithm != HashSHA512 {
		t.Errorf("StrongestHash = %+v (%v)", got, ok)
	}
	f.Hashes = append(f.Hashes, Hash{Algorithm: HashSHA256, Value: strings.Repeat("c", 64)})
	if got, _ := f.StrongestHash(); got.Algorithm != HashSHA256 {
		t.Errorf("debe preferir sha256: %+v", got)
	}
	if _, ok := f.Hash(HashSHA1); !ok {
		t.Error("el sha1 publicado se conserva")
	}

	only1 := File{Hashes: []Hash{{Algorithm: HashSHA1, Value: strings.Repeat("a", 40)}}}
	if only1.Verifiable() {
		t.Error("sha1 no permite verificar integridad")
	}
	if empty := (File{}).Verifiable(); empty {
		t.Error("sin hashes no hay verificación posible")
	}
	if v, ok := (File{Hashes: []Hash{{Algorithm: HashSHA256, Value: "  AB  "}}}).Hash(HashSHA256); !ok || v != "ab" {
		t.Errorf("Hash normaliza = %q (%v)", v, ok)
	}
}

// TestPrimaryFile: se elige el artefacto primary y, si no lo hay, el primero con
// URL.
func TestPrimaryFile(t *testing.T) {
	v := Version{Files: []File{
		{Name: "fuentes.jar", URL: "https://x/fuentes.jar"},
		{Name: "principal.jar", URL: "https://x/principal.jar", Primary: true},
	}}
	got, ok := v.PrimaryFile()
	if !ok || got.Name != "principal.jar" {
		t.Errorf("PrimaryFile = %+v (%v)", got, ok)
	}

	fallback := Version{Files: []File{
		{Name: "sin-url.jar"},
		{Name: "con-url.jar", URL: "https://x/con-url.jar"},
	}}
	if got, ok := fallback.PrimaryFile(); !ok || got.Name != "con-url.jar" {
		t.Errorf("fallback = %+v (%v)", got, ok)
	}
	if _, ok := (Version{}).PrimaryFile(); ok {
		t.Error("sin archivos no hay artefacto principal")
	}
}

// TestPrerelease: solo release cuenta como estable; un canal desconocido no.
func TestPrerelease(t *testing.T) {
	for _, tc := range []struct {
		channel string
		want    bool
	}{{"", false}, {"release", false}, {"Release", false}, {"beta", true}, {"snapshot", true}, {"desconocido", true}} {
		if got := (Version{Channel: tc.channel}).Prerelease(); got != tc.want {
			t.Errorf("Prerelease(%q) = %v", tc.channel, got)
		}
	}
}

// TestPublishedLabel: sin estado no se afirma nada.
func TestPublishedLabel(t *testing.T) {
	if got := PublishedLabel(true, ""); got != "publicado" {
		t.Errorf("got %q", got)
	}
	if got := PublishedLabel(false, ""); got != "desconocido" {
		t.Errorf("got %q", got)
	}
	if got := PublishedLabel(true, "approved"); !strings.Contains(got, "approved") {
		t.Errorf("got %q", got)
	}
	if got := PublishedLabel(false, "draft"); !strings.Contains(got, "no publicado") {
		t.Errorf("got %q", got)
	}
}

// stubSource es una fuente mínima que cumple el contrato completo, para probar
// el catálogo.
type stubSource struct{ id SourceID }

func (s stubSource) ID() SourceID  { return s.id }
func (s stubSource) Label() string { return "Stub " + string(s.id) }
func (s stubSource) Search(context.Context, SearchQuery) (SearchResult, error) {
	return SearchResult{}, nil
}
func (s stubSource) Project(context.Context, string) (*Project, error) { return nil, nil }
func (s stubSource) Versions(context.Context, string, VersionFilter) ([]Version, error) {
	return nil, nil
}

// TestCatalog: registro, búsqueda y orden de alta.
func TestCatalog(t *testing.T) {
	cat := NewCatalog()
	if _, ok := cat.Get(SourceModrinth); ok {
		t.Error("catálogo vacío no debe devolver fuentes")
	}
	cat.Register(stubSource{id: SourceModrinth})
	cat.Register(stubSource{id: SourceHangar})
	cat.Register(stubSource{id: SourceModrinth}) // reemplazo, no duplicado

	ids := cat.IDs()
	if len(ids) != 2 || ids[0] != SourceModrinth || ids[1] != SourceHangar {
		t.Errorf("IDs = %v", ids)
	}
	if labels := cat.Labels(); len(labels) != 2 || !strings.Contains(labels[1], "hangar") {
		t.Errorf("Labels = %v", labels)
	}
	if src, ok := cat.Get(SourceHangar); !ok || src.Label() != "Stub hangar" {
		t.Errorf("Get = %v (%v)", src, ok)
	}
}

// TestNormalizeDeclared: ordena, deduplica sin distinguir mayúsculas y descarta
// vacíos.
func TestNormalizeDeclared(t *testing.T) {
	got := NormalizeDeclared([]string{"paper", " Paper ", "", "fabric", "FABRIC"})
	if len(got) != 2 || got[0] != "fabric" || got[1] != "paper" {
		t.Errorf("NormalizeDeclared = %v", got)
	}
	if len(NormalizeDeclared(nil)) != 0 {
		t.Error("nil debe dar lista vacía")
	}
}

// TestDependencyKey: la clave prefiere el identificador de proyecto.
func TestDependencyKey(t *testing.T) {
	d := Dependency{ProjectID: "ABC", Name: "Nombre"}
	if d.Key() != "abc" {
		t.Errorf("Key = %q", d.Key())
	}
	if (Dependency{Name: " Nombre "}).Key() != "nombre" {
		t.Error("Key debe caer al nombre")
	}
	if (Dependency{}).Key() != "" {
		t.Error("sin datos no hay clave")
	}
}

// TestWrapTransport: el error del transporte se traduce a códigos estables de
// badomens sin filtrar cuerpos de respuesta.
func TestWrapTransport(t *testing.T) {
	cases := []struct {
		upstream string
		code     errx.Code
		reason   string
	}{
		{engines.CodeNotFound, errx.CodeNotFound, ReasonNotFound},
		{engines.CodeTimeout, errx.CodeUnavailable, ReasonRed},
		{engines.CodeRateLimited, errx.CodeUnavailable, ReasonUpstream},
		{engines.CodeUpstreamUnavailable, errx.CodeUnavailable, ReasonUpstream},
		{engines.CodeResponseTooLarge, errx.CodeUnavailable, ReasonRespuestaExcesiva},
		{engines.CodeSchemaMismatch, errx.CodeUnavailable, ReasonRespuestaInvalida},
		{engines.CodeInvalidResponse, errx.CodeUnavailable, ReasonRespuestaInvalida},
	}
	for _, tc := range cases {
		upstreamErr := &engines.TransportError{
			Code: tc.upstream, Provider: "modrinth", Operation: "get_project",
			URL: "https://api.modrinth.com/v2/project/x", HTTPStatus: 404, Retryable: false,
			Err: errors.New("detalle interno"),
		}
		err := WrapTransport(SourceModrinth, "get_project", upstreamErr)
		if !errx.Is(err, tc.code) {
			t.Errorf("%s: código = %v, quiero %s", tc.upstream, err, tc.code)
		}
		d := errx.Details(err)
		if d["reason"] != tc.reason {
			t.Errorf("%s: reason = %v, quiero %s", tc.upstream, d["reason"], tc.reason)
		}
		if d["provider"] != "modrinth" || d["operation"] != "get_project" {
			t.Errorf("%s: faltan provider/operation: %v", tc.upstream, d)
		}
	}

	// Un error no tipado se clasifica como fallo de red.
	plain := WrapTransport(SourceHangar, "search_projects", errors.New("dial tcp: fallo"))
	if !errx.Is(plain, errx.CodeUnavailable) || errx.Details(plain)["reason"] != ReasonRed {
		t.Errorf("error no tipado = %v", plain)
	}
	if WrapTransport(SourceHangar, "x", nil) != nil {
		t.Error("nil debe devolver nil")
	}
}

// TestInvalidResponseYNotFound: errores propios de los adaptadores.
func TestInvalidResponseYNotFound(t *testing.T) {
	inv := InvalidResponse(SourceModrinth, "list_project_versions", "faltan claves")
	if !errx.Is(inv, errx.CodeUnavailable) || errx.Details(inv)["reason"] != ReasonRespuestaInvalida {
		t.Errorf("InvalidResponse = %v", inv)
	}
	nf := NotFound(SourceHangar, "el proyecto", "  ")
	if !errx.Is(nf, errx.CodeNotFound) || errx.Details(nf)["reason"] != ReasonNotFound {
		t.Errorf("NotFound = %v", nf)
	}
	if !strings.Contains(nf.Error(), "desconocido") {
		t.Errorf("una referencia vacía debe mostrarse como desconocida: %v", nf)
	}
}
