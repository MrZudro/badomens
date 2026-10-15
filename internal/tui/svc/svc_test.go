package svc

import (
	"context"
	"testing"

	"github.com/MrZudro/badomens/internal/instance"
)

func fakeSpec(slug string) *instance.Spec {
	return &instance.Spec{
		Slug: slug, Name: "N " + slug, Engine: "paper", Version: "1.21.6",
		Memory: "4G", Port: 25565, MaxPlayers: 20,
	}
}

func TestNewFakeListaYDetalla(t *testing.T) {
	s := NewFake([]*instance.Spec{fakeSpec("alpha"), fakeSpec("beta")})
	rows, err := s.Instances.Rows(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Slug != "alpha" || rows[1].Slug != "beta" {
		t.Fatalf("filas: %+v", rows)
	}
	d, err := s.Instances.Detail(context.Background(), "alpha")
	if err != nil || d.Spec.Slug != "alpha" {
		t.Fatalf("detalle: %+v err=%v", d, err)
	}
	if _, err := s.Instances.Detail(context.Background(), "nope"); err == nil {
		t.Fatal("detalle inexistente debe fallar")
	}
}

func TestFakeLifecycleRegistra(t *testing.T) {
	s := NewFake([]*instance.Spec{fakeSpec("alpha")})
	lf := s.Lifecycle.(*FakeLifecycle)
	ctx := context.Background()
	_ = lf.Start(ctx, "alpha")
	_ = lf.Stop(ctx, "alpha")
	got := lf.Calls()
	if len(got) != 2 || got[0] != "start:alpha" || got[1] != "stop:alpha" {
		t.Fatalf("llamadas: %v", got)
	}
}

func TestFakePuertosYdocker(t *testing.T) {
	s := NewFake(nil)
	res, err := s.Ports.Check(context.Background(), 25565)
	if err != nil || !res.Free {
		t.Fatalf("puertos fake: %+v err=%v", res, err)
	}
	if d, _ := s.DockerState.Status(context.Background()); !d.State.Ready() {
		t.Fatalf("docker fake debe estar OK: %+v", d)
	}
}
