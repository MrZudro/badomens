package engines

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPaperProviderListAndResolve(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/versions":
			_, _ = w.Write([]byte(`{"versions":["1.21.4","1.21.5","1.21.6"]}`))
		case "/versions/1.21.6/builds":
			_, _ = w.Write([]byte(`{"builds":[{"build":156,"channel":"default"},{"build":157,"channel":"default"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := DefaultClient()
	client.Timeout = 5 * 1_000_000_000
	p := &paperProvider{engine: Paper, client: client, base: srv.URL}

	list, err := p.ListVersions(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("list = %d versiones", len(list))
	}

	res, err := p.Resolve(context.Background(), "1.21.6")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Version != "1.21.6" || res.Pins["PAPER_BUILD"] != "157" {
		t.Fatalf("resolved = %+v", res)
	}
	if res, err := p.Resolve(context.Background(), "9.9.9"); err == nil {
		t.Fatalf("resolver 9.9.9 debía fallar, got %+v", res)
	}
	if calls != 4 {
		t.Fatalf("llamadas = %d; quiero 4", calls)
	}
}

func TestVanillaProviderParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"versions":[
			{"id":"1.21.4","type":"release"},
			{"id":"1.21.3-pre1","type":"snapshot"},
			{"id":"1.21.3","type":"release"},
			{"id":"1.21.2","type":"release"}
		]}`))
	}))
	defer srv.Close()

	client := DefaultClient()
	client.Timeout = 5 * 1_000_000_000
	p := &vanillaProvider{engine: Vanilla, client: client, base: srv.URL}

	list, err := p.ListVersions(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// Las snapshots no deben contarse en la lista pública de releases.
	for _, v := range list {
		if v.Version == "1.21.3-pre1" {
			t.Fatalf("no se esperaba una snapshot en la lista: %+v", list)
		}
	}

	res, err := p.Resolve(context.Background(), "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Version != "1.21.4" {
		t.Fatalf("latest = %s", res.Version)
	}
}

func TestPickVersion(t *testing.T) {
	list := []Version{
		{Version: "1.21.3", Stable: true},
		{Version: "1.21.4", Stable: true},
	}
	v, err := pickVersion(list, "1.21.3")
	if err != nil || v.Version != "1.21.3" {
		t.Fatalf("pick: %v %+v", err, v)
	}
	v, err = pickVersion(list, "")
	if err != nil || v.Version != "1.21.4" {
		t.Fatalf("pick latest: %v %+v", err, v)
	}
}
