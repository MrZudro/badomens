package engines

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// La cobertura del proveedor Paper vive ahora en paper_test.go (Fill API v3):
// aquí se retiró el caso que usaba el esquema v2, ya retirado por el proveedor.

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
	v, err := pickVersion("paper", list, "1.21.3")
	if err != nil || v.Version != "1.21.3" {
		t.Fatalf("pick: %v %+v", err, v)
	}
	v, err = pickVersion("paper", list, "")
	if err != nil || v.Version != "1.21.4" {
		t.Fatalf("pick latest: %v %+v", err, v)
	}
}
