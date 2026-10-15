package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/spf13/cobra"
)

// newCatalogCmd agrupa los comandos de estado y refresco del catálogo de
// versiones (Fase 4). Son ADITIVOS: no alteran el contrato JSON existente.
func newCatalogCmd(e *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Estado y refresco del catálogo de versiones de cada motor",
	}
	cmd.AddCommand(newCatalogStatusCmd(e), newCatalogRefreshCmd(e))
	return cmd
}

// catalogCache construye la caché de catálogos sobre el data dir configurado.
// Es la MISMA ruta que usan `engine list` y `engine versions`.
func (e *Env) catalogCache() (*engines.Cache, error) {
	cfg, err := e.requireConfig()
	if err != nil {
		return nil, err
	}
	cache := engines.NewCache(cfg.DataDir, reg())
	if d, derr := time.ParseDuration(cfg.Versions.CacheTTL); derr == nil && d > 0 {
		cache.TTL = d
	}
	return cache, nil
}

// loadCatalog devuelve las versiones de un motor por la ruta ÚNICA de caché y
// normalización. Con refresh=true se fuerza una consulta al proveedor y se
// valida antes de escribir (conservando el último catálogo válido si falla).
func (e *Env) loadCatalog(ctx context.Context, engine string, opts engines.VersionOpts, refresh bool) ([]engines.Version, bool, engines.CatalogStatus, error) {
	cache, err := e.catalogCache()
	if err != nil {
		return nil, false, engines.CatalogStatus{}, err
	}
	if refresh {
		all, status, rerr := cache.Refresh(ctx, engine)
		if rerr != nil {
			// El catálogo previo (si lo hay) sigue disponible: el estado lo dice.
			return nil, status.State == engines.CatalogStale, status, rerr
		}
		return engines.Filter(all, opts), status.State == engines.CatalogStale, status, nil
	}
	versions, stale, verr := cache.Versions(ctx, engine, opts)
	if verr != nil {
		return nil, false, cache.Status(engine), verr
	}
	return versions, stale, cache.Status(engine), nil
}

// catalogEngineNames valida el motor pedido o devuelve todos en orden estable.
func catalogEngineNames(args []string) ([]string, error) {
	if len(args) == 1 {
		name := strings.ToLower(strings.TrimSpace(args[0]))
		if _, ok := engines.ByName(name); !ok {
			return nil, errx.Usage("motor desconocido "+name,
				"usa uno de: "+strings.Join(engineNames(), ", "))
		}
		return []string{name}, nil
	}
	return engineNames(), nil
}

// catalogStatusData serializa el estado de un catálogo.
func catalogStatusData(st engines.CatalogStatus) map[string]any {
	data := map[string]any{
		"engine":      st.Engine,
		"state":       string(st.State),
		"source":      st.Source,
		"versions":    st.Versions,
		"ttl":         st.TTL,
		"validated":   st.Validated,
		"usable":      st.Usable(),
		"age":         st.Age,
		"age_seconds": st.AgeSeconds,
	}
	if st.Provider != "" {
		data["provider"] = st.Provider
	}
	if st.Schema != "" {
		data["schema"] = st.Schema
	}
	if !st.FetchedAt.IsZero() {
		data["fetched_at"] = st.FetchedAt
	}
	if !st.UpdatedAt.IsZero() {
		data["updated_at"] = st.UpdatedAt
	}
	if st.Recommended != "" {
		data["recommended"] = st.Recommended
	}
	if st.Warning != "" {
		data["warning"] = st.Warning
	}
	return data
}

// catalogStatusHuman redacta una línea legible del estado.
func catalogStatusHuman(st engines.CatalogStatus) string {
	line := fmt.Sprintf("  %-9s %-11s %d versiones", st.Engine, st.State, st.Versions)
	if st.Age != "" {
		line += " · " + st.Age
	}
	if st.Recommended != "" {
		line += " · recomendada " + st.Recommended
	}
	if st.Warning != "" {
		line += "\n            ⚠ " + st.Warning
	}
	return line
}

func newCatalogStatusCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "status [motor]",
		Short: "Estado del catálogo en caché (fresca, vencida, inválida, no disponible)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cache, err := e.catalogCache()
			if err != nil {
				return e.finish(result.FromError("catalog status", err), "")
			}
			names, err := catalogEngineNames(args)
			if err != nil {
				return e.finish(result.FromError("catalog status", err), "")
			}
			out := make([]map[string]any, 0, len(names))
			human := "Catálogo:"
			degraded := false
			for _, name := range names {
				st := cache.Status(name)
				out = append(out, catalogStatusData(st))
				human += "\n" + catalogStatusHuman(st)
				if st.State == engines.CatalogInvalid || st.State == engines.CatalogUnavailable {
					degraded = true
				}
			}
			data := map[string]any{"catalogs": out}
			if degraded {
				return e.finish(result.Warn("catalog status",
					"hay catálogos no disponibles o inválidos", data), human)
			}
			return e.finish(result.OK("catalog status",
				fmt.Sprintf("estado del catálogo de %d motor(es)", len(names)), data), human)
		},
	}
}

func newCatalogRefreshCmd(e *Env) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "refresh [motor]",
		Short: "Fuerza la descarga, validación y guardado del catálogo",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cache, err := e.catalogCache()
			if err != nil {
				return e.finish(result.FromError("catalog refresh", err), "")
			}
			var names []string
			switch {
			case all, len(args) == 0:
				names = engineNames()
			default:
				names, err = catalogEngineNames(args)
				if err != nil {
					return e.finish(result.FromError("catalog refresh", err), "")
				}
			}
			out := make([]map[string]any, 0, len(names))
			human := "Refresco del catálogo:"
			failures := make([]string, 0)
			for _, name := range names {
				versions, st, rerr := cache.Refresh(cmd.Context(), name)
				entry := catalogStatusData(st)
				entry["engine"] = name
				if rerr != nil {
					entry["error"] = rerr.Error()
					failures = append(failures, name)
					human += fmt.Sprintf("\n  %-9s ✖ %s", name, rerr)
					out = append(out, entry)
					continue
				}
				entry["versions"] = len(versions)
				human += fmt.Sprintf("\n  %-9s ✔ %d versiones (%s)", name, len(versions), st.State)
				out = append(out, entry)
			}
			data := map[string]any{"catalogs": out}
			if len(failures) > 0 {
				return e.finish(result.Warn("catalog refresh",
					"no se pudo refrescar: "+strings.Join(failures, ", "), data), human)
			}
			return e.finish(result.OK("catalog refresh",
				fmt.Sprintf("catálogo refrescado para %d motor(es)", len(names)), data), human)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "refrescar todos los motores")
	return cmd
}
