// Comando gen-versions regenera el catálogo embebido (internal/engines/embedded.json)
// consultando los catálogos oficiales. Uso: `make gen-versions` o
// `go run ./internal/engines/gen`. Es la red de seguridad cuando no hay red en
// el primer arranque (RF-5).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/MrZudro/badomens/internal/engines"
)

func main() {
	reg := engines.NewRegistry(engines.DefaultClient())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	out := map[string][]engines.Version{}
	for _, e := range engines.Order {
		p, err := reg.Provider(e.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "aviso: %s: %v\n", e.Name, err)
			continue
		}
		list, err := p.ListVersions(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "aviso: %s: %v\n", e.Name, err)
			continue
		}
		enriched := engines.Enrich(e.Name, list)
		if len(enriched) > 40 {
			enriched = enriched[len(enriched)-40:]
		}
		out[e.Name] = enriched
		fmt.Fprintf(os.Stderr, "ok: %s (%d versiones)\n", e.Name, len(enriched))
	}

	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	raw = append(raw, '\n')
	path := filepath.Join("internal", "engines", "embedded.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "escrito", path)
}
