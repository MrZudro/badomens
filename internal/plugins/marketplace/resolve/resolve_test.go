package resolve

import (
	"context"
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/plugins"
	"github.com/MrZudro/badomens/internal/plugins/marketplace"
)

// stubSource es una fuente de mentira: el resolvedor no debe tocar la red, así
// que se le da un marketplace controlado.
type stubSource struct {
	project    *marketplace.Project
	projectErr error
	versions   []marketplace.Version
	versionErr error
	filter     marketplace.VersionFilter
	calls      int
}

func (s *stubSource) ID() marketplace.SourceID { return "stub" }
func (s *stubSource) Label() string            { return "Stub" }
func (s *stubSource) Search(context.Context, marketplace.SearchQuery) (marketplace.SearchResult, error) {
	return marketplace.SearchResult{}, nil
}

func (s *stubSource) Project(context.Context, string) (*marketplace.Project, error) {
	s.calls++
	if s.projectErr != nil {
		return nil, s.projectErr
	}
	return s.project, nil
}

func (s *stubSource) Versions(_ context.Context, _ string, f marketplace.VersionFilter) ([]marketplace.Version, error) {
	s.filter = f
	if s.versionErr != nil {
		return nil, s.versionErr
	}
	return s.versions, nil
}

// paperProject es un plugin de Paper publicado.
func paperProject() *marketplace.Project {
	return &marketplace.Project{
		Source:       "stub",
		ID:           "P1OZGk5p",
		Slug:         "viaversion",
		Title:        "ViaVersion",
		Kind:         marketplace.KindPlugin,
		Loaders:      []string{"paper"},
		GameVersions: []string{"1.21.1"},
		Status:       "approved",
		Published:    true,
	}
}

// version construye una versión de prueba.
func version(number, published string, type_ string, loaders []string, mc []string, hash marketplace.Hash) marketplace.Version {
	v := marketplace.Version{
		Source:        "stub",
		ProjectID:     "P1OZGk5p",
		ID:            "id-" + number,
		Number:        number,
		Channel:       type_,
		Loaders:       loaders,
		GameVersions:  mc,
		Status:        "listed",
		Published:     true,
		DatePublished: published,
		Files: []marketplace.File{{
			Name:    "viaversion-" + number + ".jar",
			URL:     "https://cdn.example/viaversion-" + number + ".jar",
			Size:    1024,
			Primary: true,
		}},
	}
	if hash.Value != "" {
		v.Files[0].Hashes = []marketplace.Hash{hash}
	}
	return v
}

func env() marketplace.Environment {
	return marketplace.Environment{Engine: "paper", Loader: "paper", GameVersion: "1.21.1"}
}

func sha512(value string) marketplace.Hash {
	return marketplace.Hash{Algorithm: marketplace.HashSHA512, Value: value}
}

// newStub monta una fuente con un proyecto de Paper y las versiones dadas.
func newStub(versions ...marketplace.Version) *stubSource {
	return &stubSource{project: paperProject(), versions: versions}
}

// TestResuelveLaMasRecientePublicada: con varias versiones compatibles se elige
// la más reciente por fecha publicada, no el orden en que llegaron.
func TestResuelveLaMasRecientePublicada(t *testing.T) {
	old := version("5.4.0", "2025-01-01T00:00:00Z", "release", []string{"paper"}, []string{"1.21.1"}, sha512(strings.Repeat("a", 128)))
	newer := version("5.5.0", "2025-06-01T00:00:00Z", "release", []string{"paper"}, []string{"1.21.1"}, sha512(strings.Repeat("b", 128)))
	src := newStub(old, newer) // llegan desordenadas a propósito

	res, err := Resolve(context.Background(), Request{Source: src, Project: "viaversion", Env: env()})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !res.Complete {
		t.Error("la resolución debería estar completa")
	}
	if res.Version.Number != "5.5.0" {
		t.Errorf("versión elegida = %q, quiero 5.5.0", res.Version.Number)
	}
	if res.Compat != marketplace.Compatible {
		t.Errorf("compat = %q", res.Compat)
	}
	if res.Hash.Algorithm != marketplace.HashSHA512 || res.File.Name != "viaversion-5.5.0.jar" {
		t.Errorf("hash/archivo = %+v / %q", res.Hash, res.File.Name)
	}
	if src.filter.Loaders[0] != "paper" || src.filter.GameVersions[0] != "1.21.1" {
		t.Errorf("el filtro no lleva el entorno: %+v", src.filter)
	}
}

// TestResolucionIncompletaSinChecksum: sin checksum utilizable la resolución FALLA
// (AC-06) y solo se acepta si el llamador asume el riesgo de forma explícita.
func TestResolucionIncompletaSinChecksum(t *testing.T) {
	only1 := version("5.5.0", "2025-06-01T00:00:00Z", "release", []string{"paper"}, []string{"1.21.1"},
		marketplace.Hash{Algorithm: marketplace.HashSHA1, Value: strings.Repeat("c", 40)})
	src := newStub(only1)

	res, err := Resolve(context.Background(), Request{Source: src, Project: "viaversion", Env: env()})
	if err == nil {
		t.Fatal("se esperaba error: sha1 no es un checksum utilizable")
	}
	if res == nil || res.Complete {
		t.Fatalf("resolución = %+v", res)
	}
	if got := reasonOf(err); got != ReasonChecksum {
		t.Errorf("reason = %q, quiero %q", got, ReasonChecksum)
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("mensaje = %q", err.Error())
	}
	if len(res.Problems) == 0 || !res.Problems[0].Fatal {
		t.Errorf("problemas = %+v", res.Problems)
	}

	// Con el permiso explícito, se resuelve y queda como aviso.
	res2, err2 := Resolve(context.Background(), Request{
		Source: src, Project: "viaversion", Env: env(), AllowUnverified: true,
	})
	if err2 != nil {
		t.Fatalf("Resolve con AllowUnverified: %v", err2)
	}
	if !res2.Complete {
		t.Error("debería completarse con el permiso explícito")
	}
	if len(res2.Warnings) == 0 || !strings.Contains(res2.Warnings[0], "SIN verificación") {
		t.Errorf("avisos = %v", res2.Warnings)
	}
	if res2.Hash.Value != "" {
		t.Errorf("no debe fingir un checksum: %+v", res2.Hash)
	}
}

// TestResolucionIncompletaSinEntorno: sin loader o sin versión de Minecraft no
// hay resolución posible: no se puede afirmar compatibilidad.
func TestResolucionIncompletaSinEntorno(t *testing.T) {
	src := newStub()
	// Sin motor ni loader: no hay nada que comparar con lo declarado.
	_, err := Resolve(context.Background(), Request{Source: src, Project: "viaversion",
		Env: marketplace.Environment{GameVersion: "1.21.1"}})
	if got := reasonOf(err); got != ReasonEntorno {
		t.Errorf("sin loader: reason = %q", got)
	}
	// Sin versión de Minecraft: la compatibilidad queda indeterminada.
	_, err = Resolve(context.Background(), Request{Source: src, Project: "viaversion",
		Env: marketplace.Environment{Engine: "paper", Loader: "paper"}})
	if got := reasonOf(err); got != ReasonEntorno {
		t.Errorf("sin versión de Minecraft: reason = %q", got)
	}
	// El motor por sí solo ya determina el loader: eso no es un entorno
	// incompleto.
	_, err = Resolve(context.Background(), Request{Source: newStub(), Project: "viaversion",
		Env: marketplace.Environment{Engine: "paper", GameVersion: "1.21.1"}})
	if got := reasonOf(err); got != ReasonSinVersion {
		t.Errorf("con motor y versión se debe llegar a resolver: reason = %q", got)
	}
}

// TestResolucionIncompletaSinVersion: si el proveedor no publica ninguna versión
// para ese loader y esa versión de Minecraft, se falla con explicación.
func TestResolucionIncompletaSinVersion(t *testing.T) {
	src := newStub()
	res, err := Resolve(context.Background(), Request{Source: src, Project: "viaversion", Env: env()})
	if got := reasonOf(err); got != ReasonSinVersion {
		t.Errorf("reason = %q", got)
	}
	if res.Complete {
		t.Error("no puede estar completa")
	}
	if !strings.Contains(res.Problems[0].Message, "paper") {
		t.Errorf("mensaje = %q", res.Problems[0].Message)
	}
}

// TestCompatibilidadDesconocidaNoSeConvierteEnCompatible: una versión sin datos
// de loader no puede seleccionarse.
func TestCompatibilidadDesconocidaNoSeConvierteEnCompatible(t *testing.T) {
	src := newStub(version("5.5.0", "2025-06-01T00:00:00Z", "release", nil, nil, sha512(strings.Repeat("d", 128))))
	res, err := Resolve(context.Background(), Request{Source: src, Project: "viaversion", Env: env()})
	if err == nil {
		t.Fatalf("se esperaba error, resolución = %+v", res)
	}
	if got := reasonOf(err); got != ReasonSinVersion {
		t.Errorf("reason = %q", got)
	}
}

// TestResolucionIncompletaSinArchivo: una versión sin artefactos no se puede
// instalar.
func TestResolucionIncompletaSinArchivo(t *testing.T) {
	v := version("5.5.0", "2025-06-01T00:00:00Z", "release", []string{"paper"}, []string{"1.21.1"}, sha512(strings.Repeat("e", 128)))
	v.Files = nil
	_, err := Resolve(context.Background(), Request{Source: newStub(v), Project: "viaversion", Env: env()})
	if got := reasonOf(err); got != ReasonSinArchivo {
		t.Errorf("reason = %q", got)
	}
}

// TestResolucionIncompletaArchivoNoInstalable: un artefacto que no es .jar/.zip
// se rechaza antes de descargar nada.
func TestResolucionIncompletaArchivoNoInstalable(t *testing.T) {
	v := version("5.5.0", "2025-06-01T00:00:00Z", "release", []string{"paper"}, []string{"1.21.1"}, sha512(strings.Repeat("f", 128)))
	v.Files[0].Name = "viaversion-5.5.0.exe"
	_, err := Resolve(context.Background(), Request{Source: newStub(v), Project: "viaversion", Env: env()})
	if got := reasonOf(err); got != ReasonArchivoNoInstalable {
		t.Errorf("reason = %q", got)
	}
}

// TestVersionExacta: pedir una versión concreta la exige.
func TestVersionExacta(t *testing.T) {
	a := version("5.4.0", "2025-01-01T00:00:00Z", "release", []string{"paper"}, []string{"1.21.1"}, sha512(strings.Repeat("a", 128)))
	b := version("5.5.0", "2025-06-01T00:00:00Z", "release", []string{"paper"}, []string{"1.21.1"}, sha512(strings.Repeat("b", 128)))
	src := newStub(a, b)

	res, err := Resolve(context.Background(), Request{Source: src, Project: "viaversion", Env: env(), Version: "5.4.0"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Version.Number != "5.4.0" {
		t.Errorf("versión = %q", res.Version.Number)
	}

	_, err = Resolve(context.Background(), Request{Source: src, Project: "viaversion", Env: env(), Version: "9.9.9"})
	if got := reasonOf(err); got != ReasonVersionInexistente {
		t.Errorf("reason = %q", got)
	}
	if !errx.Is(err, errx.CodeNotFound) {
		t.Errorf("código = %v", err)
	}
}

// TestVersionExactaIncompatible: si existe pero no declara el entorno, se dice
// exactamente eso.
func TestVersionExactaIncompatible(t *testing.T) {
	v := version("5.5.0", "2025-06-01T00:00:00Z", "release", []string{"neoforge"}, []string{"1.21.1"}, sha512(strings.Repeat("b", 128)))
	_, err := Resolve(context.Background(), Request{
		Source: newStub(v), Project: "viaversion", Env: env(), Version: "5.5.0",
	})
	if got := reasonOf(err); got != ReasonVersionIncompatible {
		t.Errorf("reason = %q", got)
	}
}

// TestCanalNoPermitido: por defecto solo se aceptan lanzamientos estables.
func TestCanalNoPermitido(t *testing.T) {
	beta := version("5.6.0-beta1", "2025-06-01T00:00:00Z", "beta", []string{"paper"}, []string{"1.21.1"}, sha512(strings.Repeat("b", 128)))
	src := newStub(beta)

	_, err := Resolve(context.Background(), Request{Source: src, Project: "viaversion", Env: env()})
	if got := reasonOf(err); got != ReasonSinVersion {
		t.Errorf("reason = %q", got)
	}
	res, err := Resolve(context.Background(), Request{
		Source: src, Project: "viaversion", Env: env(), AllowPrerelease: true,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Version.Number != "5.6.0-beta1" {
		t.Errorf("versión = %q", res.Version.Number)
	}
}

// TestNoPublicadoNoSeInstala: un proyecto o una versión sin publicar no se
// resuelve.
func TestNoPublicadoNoSeInstala(t *testing.T) {
	src := newStub()
	proj := paperProject()
	proj.Published = false
	proj.Status = "archived"
	src.project = proj
	_, err := Resolve(context.Background(), Request{Source: src, Project: "viaversion", Env: env()})
	if got := reasonOf(err); got != ReasonNoPublicado {
		t.Errorf("reason = %q", got)
	}

	unpub := version("5.5.0", "2025-06-01T00:00:00Z", "release", []string{"paper"}, []string{"1.21.1"}, sha512(strings.Repeat("b", 128)))
	unpub.Published = false
	unpub.Status = "archived"
	_, err = Resolve(context.Background(), Request{Source: newStub(unpub), Project: "viaversion", Env: env()})
	if got := reasonOf(err); got != ReasonSinVersion {
		t.Errorf("reason = %q", got)
	}
}

// TestTipoNoInstalable: un modpack no se instala en un servidor de plugins.
func TestTipoNoInstalable(t *testing.T) {
	src := newStub()
	proj := paperProject()
	proj.Kind = marketplace.KindModpack
	src.project = proj
	_, err := Resolve(context.Background(), Request{Source: src, Project: "viaversion", Env: env()})
	if got := reasonOf(err); got != ReasonTipo {
		t.Errorf("reason = %q", got)
	}
}

// TestDependenciaObligatoriaAusente: se advierte y NO se resuelve como exitoso
// salvo permiso explícito; nunca se instala la dependencia sola.
func TestDependenciaObligatoriaAusente(t *testing.T) {
	v := version("5.5.0", "2025-06-01T00:00:00Z", "release", []string{"paper"}, []string{"1.21.1"}, sha512(strings.Repeat("b", 128)))
	v.Dependencies = []marketplace.Dependency{
		{ProjectID: "YlKdE5VK", Name: "ViaBackwards", Kind: marketplace.DepRequired},
		{ProjectID: "TbHIxhx5", Name: "Opcional", Kind: marketplace.DepOptional},
	}
	res, err := Resolve(context.Background(), Request{Source: newStub(v), Project: "viaversion", Env: env()})
	if err == nil {
		t.Fatalf("se esperaba error por dependencia ausente, resolución = %+v", res)
	}
	if got := reasonOf(err); got != ReasonDependencia {
		t.Errorf("reason = %q", got)
	}
	if !strings.Contains(err.Error(), "ViaBackwards") {
		t.Errorf("mensaje = %q", err.Error())
	}
	if len(res.Dependencies) != 2 || res.Dependencies[0].Status != DepAusente {
		t.Errorf("dependencias = %+v", res.Dependencies)
	}
	if res.Dependencies[1].Status != DepOpcional {
		t.Errorf("una dependencia opcional no condiciona: %+v", res.Dependencies[1])
	}

	// Con permiso explícito queda como aviso.
	res2, err2 := Resolve(context.Background(), Request{
		Source: newStub(v), Project: "viaversion", Env: env(), AllowMissingDeps: true,
	})
	if err2 != nil {
		t.Fatalf("Resolve con AllowMissingDeps: %v", err2)
	}
	if len(res2.Warnings) == 0 {
		t.Error("debería avisar de la dependencia ausente")
	}
}

// TestDependenciaObligatoriaSinIdentificador: si el proveedor dice que hay una
// dependencia obligatoria pero no dice cuál, no se puede verificar y la
// resolución NO se da por buena.
func TestDependenciaObligatoriaSinIdentificador(t *testing.T) {
	v := version("5.5.0", "2025-06-01T00:00:00Z", "release", []string{"paper"}, []string{"1.21.1"}, sha512(strings.Repeat("b", 128)))
	v.Dependencies = []marketplace.Dependency{{Kind: marketplace.DepRequired}}
	res, err := Resolve(context.Background(), Request{Source: newStub(v), Project: "viaversion", Env: env()})
	if err == nil {
		t.Fatalf("se esperaba error, resolución = %+v", res)
	}
	if got := reasonOf(err); got != ReasonDependencia {
		t.Errorf("reason = %q", got)
	}
	if len(res.Dependencies) != 1 || res.Dependencies[0].Status != DepNoVerificable {
		t.Errorf("dependencias = %+v", res.Dependencies)
	}
	if !strings.Contains(res.Problems[0].Message, "no identifica") {
		t.Errorf("mensaje = %q", res.Problems[0].Message)
	}
}

// TestDependenciaSatisfechaEIncompatible: el cruce con lo instalado usa el
// registro de procedencia; una dependencia declarada incompatible que está
// instalada bloquea.
func TestDependenciaSatisfechaEIncompatible(t *testing.T) {
	v := version("5.5.0", "2025-06-01T00:00:00Z", "release", []string{"paper"}, []string{"1.21.1"}, sha512(strings.Repeat("b", 128)))
	v.Dependencies = []marketplace.Dependency{
		{ProjectID: "YlKdE5VK", Name: "ViaBackwards", Kind: marketplace.DepRequired},
		{ProjectID: "NpvuJQoq", Name: "Conflicto", Kind: marketplace.DepIncompatible},
	}
	installed := []plugins.Record{
		{Name: "ViaBackwards-5.0.0.jar", Version: "5.0.0", Origin: plugins.Origin{Source: "modrinth", ProjectID: "YlKdE5VK", ProjectSlug: "viabackwards"}},
	}
	res, err := Resolve(context.Background(), Request{
		Source: newStub(v), Project: "viaversion", Env: env(), Installed: installed,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Dependencies[0].Status != DepSatisfecha {
		t.Errorf("dependencia obligatoria = %+v", res.Dependencies[0])
	}
	if res.Dependencies[1].Status != DepSatisfecha {
		t.Errorf("dependencia incompatible no instalada = %+v", res.Dependencies[1])
	}

	// Ahora el proyecto declarado incompatible SÍ está instalado.
	conflict := append(installed, plugins.Record{
		Name: "Conflicto-1.0.0.jar", Version: "1.0.0",
		Origin: plugins.Origin{Source: "modrinth", ProjectID: "NpvuJQoq"},
	})
	_, err = Resolve(context.Background(), Request{
		Source: newStub(v), Project: "viaversion", Env: env(), Installed: conflict,
	})
	if got := reasonOf(err); got != ReasonDependencia {
		t.Errorf("reason = %q", got)
	}
}

// TestDependenciaPorNombre: cuando el proveedor solo da nombres (Hangar), el
// cruce se hace contra el nombre del archivo y el proyecto registrado.
func TestDependenciaPorNombre(t *testing.T) {
	v := version("2.0", "2025-06-01T00:00:00Z", "release", []string{"paper"}, []string{"1.21.1"}, sha512(strings.Repeat("b", 128)))
	v.Dependencies = []marketplace.Dependency{{Name: "PlaceholderAPI", Kind: marketplace.DepRequired}}
	installed := []plugins.Record{
		{Name: "PlaceholderAPI-2.11.6.jar", Origin: plugins.Origin{Source: "hangar", ProjectName: "PlaceholderAPI"}},
	}
	res, err := Resolve(context.Background(), Request{
		Source: newStub(v), Project: "viaversion", Env: env(), Installed: installed,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Dependencies[0].Status != DepSatisfecha {
		t.Errorf("dependencia = %+v", res.Dependencies[0])
	}
}

// TestFuenteAusenteYProyectoVacio: errores de uso explícitos.
func TestFuenteAusenteYProyectoVacio(t *testing.T) {
	if _, err := Resolve(context.Background(), Request{Project: "x", Env: env()}); !errx.Is(err, errx.CodeUsage) {
		t.Errorf("sin fuente: %v", err)
	}
	if _, err := Resolve(context.Background(), Request{Source: newStub(), Project: "  ", Env: env()}); !errx.Is(err, errx.CodeUsage) {
		t.Errorf("sin proyecto: %v", err)
	}
}

// TestPropagaErrorDelProveedor: el error tipado de la fuente llega intacto.
func TestPropagaErrorDelProveedor(t *testing.T) {
	src := newStub()
	src.projectErr = errx.NotFound("no existe", "revisa el nombre")
	if _, err := Resolve(context.Background(), Request{Source: src, Project: "x", Env: env()}); !errx.Is(err, errx.CodeNotFound) {
		t.Errorf("se esperaba not_found, got %v", err)
	}
}

// reasonOf extrae el motivo estable de un error tipado.
func reasonOf(err error) string {
	if err == nil {
		return ""
	}
	d := errx.Details(err)
	if d == nil {
		return ""
	}
	r, _ := d["reason"].(string)
	return r
}
