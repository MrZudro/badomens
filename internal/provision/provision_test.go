package provision

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/instance"
)

// fakeService resuelve motor+versión como el servicio de catálogo, sin red: la
// versión pedida (o una fija) con TODOS los pins obligatorios del motor, igual
// que haría la ruta real (AC-06).
type fakeService struct{ version string }

func (s fakeService) ResolveVersion(_ context.Context, engine, version string) (engines.Resolved, error) {
	e, ok := engines.ByName(engine)
	if !ok {
		return engines.Resolved{}, errx.Usage("motor desconocido "+engine, "")
	}
	v := s.version
	if version != "" {
		v = version
	}
	pins := map[string]string{}
	for _, dep := range engines.RequiredPins(e) {
		pins[dep] = "1"
	}
	return engines.Resolved{
		Version: v, Pins: pins, Source: e.Name,
		Compatibility: engines.CompatibilityCompatible,
	}, nil
}

func testDeps(t *testing.T) (Deps, string) {
	t.Helper()
	dir := t.TempDir()
	names := make([]string, 0, len(engines.Order))
	for _, e := range engines.Order {
		names = append(names, e.Name)
	}
	cfg := &config.Config{DataDir: dir, DefaultMemory: "4G"}
	return Deps{
		Cfg:      cfg,
		Manager:  instance.New(dir, names),
		Resolver: fakeService{version: "1.21.6"},
	}, dir
}

func TestCreateInstanciaCompleta(t *testing.T) {
	deps, _ := testDeps(t)
	res, err := Create(context.Background(), deps, Request{
		Name: "Survival World", Engine: "paper", Memory: "4G", MaxPlayers: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	s := res.Spec
	if s.Slug != "survival-world" {
		t.Fatalf("slug: quiero survival-world, tengo %s", s.Slug)
	}
	if s.Engine != "paper" || s.Version != "1.21.6" {
		t.Fatalf("motor/versión: %s %s", s.Engine, s.Version)
	}
	if s.MaxPlayers != 20 || s.Port <= 0 || s.Memory != "4G" {
		t.Fatalf("campos: %+v", s)
	}
	if res.Started {
		t.Fatal("no debe arrancar sin runtime")
	}
	if _, err := Create(context.Background(), deps, Request{Name: "Survival World"}); err == nil {
		t.Fatal("la segunda creación del mismo nombre debe fallar")
	}
}

func TestCreatePorDefectos(t *testing.T) {
	deps, _ := testDeps(t)
	res, err := Create(context.Background(), deps, Request{Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	s := res.Spec
	if s.Engine != "paper" || s.Version != "1.21.6" {
		t.Fatalf("defectos: %s %s", s.Engine, s.Version)
	}
	if s.Memory != "4G" || s.Difficulty != "normal" || s.Gamemode != "survival" {
		t.Fatalf("defectos 2: %+v", s)
	}
	if s.MaxPlayers <= 0 {
		t.Fatal("max players debe tener recomendación")
	}
}

func TestCreateErrores(t *testing.T) {
	deps, _ := testDeps(t)
	cases := []Request{
		{},                          // sin nombre
		{Name: "x", Memory: "12Q"},  // memoria inválida
		{Name: "x", Engine: "nope"}, // motor inválido
		{Name: "x", MaxPlayers: -3}, // max players inválido por Validate
	}
	for i, req := range cases {
		if _, err := Create(context.Background(), deps, req); err == nil {
			t.Fatalf("caso %d debe fallar: %+v", i, req)
		}
	}
}

func TestCreateStartRequiereRuntime(t *testing.T) {
	deps, _ := testDeps(t)
	res, err := Create(context.Background(), deps, Request{Name: "demo", Start: true})
	if err == nil {
		t.Fatal("arrancar sin runtime debe fallar")
	}
	if res == nil || res.Spec == nil {
		t.Fatal("aun así debe devolver el spec guardado")
	}
	if _, err := os.Stat(deps.Manager.SpecPath(res.Spec.Slug)); err != nil {
		t.Fatalf("el spec debe quedar guardado: %v", err)
	}
}

// catalogoCambiante simula un catálogo que publica un build NUEVO en cada
// llamada (builds[i]): reproduce la carrera entre la resolución mostrada en el
// resumen y la creación (AD-02).
type catalogoCambiante struct {
	builds []string
	i      int
}

func (s *catalogoCambiante) ResolveVersion(_ context.Context, engine, version string) (engines.Resolved, error) {
	e, ok := engines.ByName(engine)
	if !ok {
		return engines.Resolved{}, errx.Usage("motor desconocido "+engine, "")
	}
	v := strings.TrimSpace(version)
	if v == "" {
		v = "1.21.6"
	}
	b := "1"
	if s.i < len(s.builds) {
		b = s.builds[s.i]
	}
	s.i++
	pins := map[string]string{}
	for _, dep := range engines.RequiredPins(e) {
		pins[dep] = b
	}
	return engines.Resolved{Version: v, Pins: pins, Source: e.Name, Compatibility: engines.CompatibilityCompatible}, nil
}

// TestCreateInyectaPinsExactos: si el llamador aporta los pins ya resueltos y
// revisados, se persiste EXACTAMENTE ese build aunque el catálogo devuelva otro
// nuevo durante la creación (AD-02/T-08).
func TestCreateInyectaPinsExactos(t *testing.T) {
	deps, _ := testDeps(t)
	camb := &catalogoCambiante{builds: []string{"1", "2"}}
	deps.Resolver = camb
	// 1) Resolución revisada por el usuario (build 1).
	reviewed, err := camb.ResolveVersion(context.Background(), "paper", "1.21.6")
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.Pins["PAPER_BUILD"] != "1" {
		t.Fatalf("la resolución revisada debe ser el build 1, tengo %q", reviewed.Pins["PAPER_BUILD"])
	}
	// 2) Creación: el catálogo ya devolvería el build 2, pero mandan los pins
	// revisados.
	res, err := Create(context.Background(), deps, Request{
		Name: "pinned", Engine: "paper", Pins: reviewed.Pins,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Spec.Pins["PAPER_BUILD"]; got != "1" {
		t.Fatalf("la configuración persistida debe usar el pin revisado (1), tengo %q", got)
	}
	if res.Spec.Version != "1.21.6" {
		t.Fatalf("versión persistida: %q", res.Spec.Version)
	}
	// El mapa inyectado no se comparte con el spec (sin mutaciones cruzadas).
	reviewed.Pins["PAPER_BUILD"] = "mutado"
	if res.Spec.Pins["PAPER_BUILD"] != "1" {
		t.Fatal("el spec no debe compartir el mapa de pins de la petición")
	}
}

// TestCreateSinPinsUsaLosDelCatalogo: sin inyección rige la resolución del
// servicio (comportamiento previo intacto).
func TestCreateSinPinsUsaLosDelCatalogo(t *testing.T) {
	deps, _ := testDeps(t)
	deps.Resolver = &catalogoCambiante{builds: []string{"7"}}
	res, err := Create(context.Background(), deps, Request{Name: "sinpins", Engine: "paper"})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Spec.Pins["PAPER_BUILD"]; got != "7" {
		t.Fatalf("sin pins inyectados debe usarse el build del catálogo (7), tengo %q", got)
	}
}

// TestCreateRechazaPinsInvalidos: un pin obligatorio vacío o «latest» se
// rechaza y no deja instancia escrita (invariante AC-06).
func TestCreateRechazaPinsInvalidos(t *testing.T) {
	for _, pin := range []string{"", "latest"} {
		deps, _ := testDeps(t)
		_, err := Create(context.Background(), deps, Request{
			Name: "bad", Engine: "paper", Pins: map[string]string{"PAPER_BUILD": pin},
		})
		if err == nil {
			t.Fatalf("el pin %q debe rechazarse", pin)
		}
		if _, serr := deps.Manager.Load("bad"); serr == nil {
			t.Fatal("una petición con pins inválidos no debe dejar instancia escrita")
		}
	}
}
