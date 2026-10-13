package provision

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/MrZudro/badomens/internal/config"
	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/instance"
)

// fakeProvider devuelve una versión fija sin red.
type fakeProvider struct {
	engine  engines.Engine
	version string
}

func (p fakeProvider) Engine() engines.Engine { return p.engine }
func (p fakeProvider) ListVersions(context.Context) ([]engines.Version, error) {
	return []engines.Version{{Version: p.version}}, nil
}

func (p fakeProvider) Resolve(_ context.Context, version string) (engines.Resolved, error) {
	v := p.version
	if version != "" {
		v = version
	}
	return engines.Resolved{Version: v}, nil
}

// fakeResolver sirve proveedores por nombre sin HTTP.
type fakeResolver struct{ m map[string]engines.Provider }

func (r fakeResolver) Provider(name string) (engines.Provider, error) {
	p, ok := r.m[name]
	if !ok {
		return nil, fmt.Errorf("motor desconocido %q", name)
	}
	return p, nil
}

func testDeps(t *testing.T) (Deps, string) {
	t.Helper()
	dir := t.TempDir()
	names := make([]string, 0, len(engines.Order))
	prov := map[string]engines.Provider{}
	for _, e := range engines.Order {
		names = append(names, e.Name)
		prov[e.Name] = fakeProvider{engine: e, version: "1.21.6"}
	}
	cfg := &config.Config{DataDir: dir, DefaultMemory: "4G"}
	return Deps{
		Cfg:      cfg,
		Manager:  instance.New(dir, names),
		Resolver: fakeResolver{m: prov},
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
