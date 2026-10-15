// Cableado de los marketplaces (fases 11.2 y 11.3) en el CLI.
//
// Este archivo NO reimplementa nada: conecta los paquetes ya probados
// (internal/plugins/marketplace y sus subpaquetes modrinth, hangar, resolve e
// install) con los subcomandos de cobra, y traduce el resultado a claves
// ADITIVAS dentro de "data", sin tocar el contrato JSON.
//
// Reglas que este cableado no relaja:
//
//   - una resolución incompleta NUNCA es un éxito (AC-06): el comando falla con
//     error tipado, deja "resolution_complete": false en data y no escribe nada;
//   - un resultado de búsqueda (y el detalle de un proyecto) NO demuestra
//     compatibilidad: se informa de la compatibilidad DECLARADA y siempre con
//     "compatibility_verified": false;
//   - "plugin install" es el único subcomando que escribe en la instancia, y lo
//     hace solo cuando la instalación ha terminado del todo (archivo + registro
//     de procedencia + lista de la instancia);
//   - "plugin updates" nunca instala ni modifica el archivo instalado.
//
// La vía de URL directa sigue siendo "plugin add", con la política de 11.1
// intacta: aquí no se toca.
package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/plugins"
	"github.com/MrZudro/badomens/internal/plugins/archive"
	"github.com/MrZudro/badomens/internal/plugins/download"
	"github.com/MrZudro/badomens/internal/plugins/marketplace"
	"github.com/MrZudro/badomens/internal/plugins/marketplace/hangar"
	"github.com/MrZudro/badomens/internal/plugins/marketplace/install"
	"github.com/MrZudro/badomens/internal/plugins/marketplace/modrinth"
	"github.com/MrZudro/badomens/internal/plugins/marketplace/resolve"
	"github.com/MrZudro/badomens/internal/result"
	"github.com/spf13/cobra"
)

// newPluginMarketCatalog es la fábrica del catálogo de marketplaces (la MISMA
// ruta que usan CLI, TUI y MCP: el catálogo se construye una vez por comando).
//
// Es una variable para que las pruebas puedan inyectar adaptadores que apunten a
// servidores httptest sin tocar la red real.
var newPluginMarketCatalog = func() (*marketplace.Catalog, error) {
	client := engines.DefaultClient()
	cat := marketplace.NewCatalog()
	cat.Register(modrinth.New(client))
	cat.Register(hangar.New(client))
	return cat, nil
}

// marketCatalog devuelve el catálogo de marketplaces.
func (e *Env) marketCatalog() (*marketplace.Catalog, error) {
	return newPluginMarketCatalog()
}

// marketClient construye el cliente de instalación de marketplaces sobre el
// data dir configurado. Reutiliza la política de 11.1 (descarga segura,
// verificación e inspección) tal cual: aquí no se define ninguna otra.
func (e *Env) marketClient(cat *marketplace.Catalog) (*install.Client, error) {
	cfg, err := e.requireConfig()
	if err != nil {
		return nil, err
	}
	installer := &plugins.Installer{DataDir: cfg.DataDir}
	return install.New(cat, installer, plugins.NewProvenance(cfg.DataDir)), nil
}

// marketEnvironment devuelve la instancia activa (si se indicó con -i) y el
// entorno declarado por ella. Sin instancia devuelve un entorno VACÍO: las
// consultas de catálogo siguen funcionando, pero la compatibilidad queda
// "desconocido" y así se informa.
func (e *Env) marketEnvironment() (*instance.Spec, marketplace.Environment, bool, error) {
	if strings.TrimSpace(e.instanceFlag) == "" {
		return nil, marketplace.Environment{}, false, nil
	}
	spec, err := e.activeSpec()
	if err != nil {
		return nil, marketplace.Environment{}, false, err
	}
	return spec, marketEnvironmentOf(spec), true, nil
}

// marketEnvironmentOf traduce la instancia a un entorno de marketplace. El
// loader es el nombre del motor: es lo que declaran los proveedores (Modrinth lo
// usa como categoría y Hangar como plataforma).
func marketEnvironmentOf(spec *instance.Spec) marketplace.Environment {
	engine := strings.ToLower(strings.TrimSpace(spec.Engine))
	return marketplace.Environment{
		Engine:      engine,
		Loader:      engine,
		GameVersion: strings.TrimSpace(spec.Version),
	}
}

// --- Seleccion de fuentes ----------------------------------------------------

// selectMarketSources devuelve las fuentes a consultar: todas las registradas o
// solo la pedida con --source.
func selectMarketSources(cat *marketplace.Catalog, want string) ([]marketplace.Source, error) {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" {
		out := make([]marketplace.Source, 0, len(cat.IDs()))
		for _, id := range cat.IDs() {
			if s, ok := cat.Get(id); ok {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil, errx.New(errx.CodeUsage, "no hay ningún marketplace registrado",
				"configura Modrinth o Hangar").WithDetail("reason", install.ReasonFuente)
		}
		return out, nil
	}
	src, ok := cat.Get(marketplace.SourceID(want))
	if !ok {
		return nil, unknownSourceError(cat, want)
	}
	return []marketplace.Source{src}, nil
}

// pickMarketSource devuelve la fuente pedida o, si no se indicó ninguna, la
// primera registrada. La elección por defecto es determinista y se informa en el
// resultado con la clave "source".
func pickMarketSource(cat *marketplace.Catalog, want string) (marketplace.Source, error) {
	if strings.TrimSpace(want) != "" {
		src, ok := cat.Get(marketplace.SourceID(strings.ToLower(strings.TrimSpace(want))))
		if !ok {
			return nil, unknownSourceError(cat, want)
		}
		return src, nil
	}
	for _, id := range cat.IDs() {
		if s, ok := cat.Get(id); ok {
			return s, nil
		}
	}
	return nil, errx.New(errx.CodeUsage, "no hay ningún marketplace registrado",
		"configura Modrinth o Hangar").WithDetail("reason", install.ReasonFuente)
}

// unknownSourceError construye el error de marketplace desconocido.
func unknownSourceError(cat *marketplace.Catalog, want string) error {
	ids := make([]string, 0, len(cat.IDs()))
	for _, id := range cat.IDs() {
		ids = append(ids, string(id))
	}
	hint := "no hay marketplaces disponibles"
	if len(ids) > 0 {
		hint = "usa uno de: " + strings.Join(ids, ", ")
	}
	return errx.New(errx.CodeUsage, "marketplace desconocido: "+want, hint).
		WithDetail("reason", install.ReasonFuente).
		WithDetail("requested", want)
}

// --- Serializacion aditiva ---------------------------------------------------

// environmentData serializa el entorno usado para calcular compatibilidad.
// Devuelve nil si no hay entorno: no se inventa ninguno.
func environmentData(env marketplace.Environment, known bool) map[string]any {
	if !known {
		return nil
	}
	return map[string]any{
		"engine":       env.Engine,
		"loader":       env.LoaderName(),
		"game_version": env.GameVersion,
	}
}

// projectData serializa un proyecto del catálogo. Nunca afirma compatibilidad
// verificada: la compatibilidad que se informa es la DECLARADA por el proveedor.
func projectData(p marketplace.Project) map[string]any {
	d := map[string]any{
		"source":                 string(p.Source),
		"id":                     p.ID,
		"slug":                   p.Slug,
		"title":                  p.Title,
		"description":            p.Description,
		"kind":                   string(p.Kind),
		"url":                    p.URL,
		"downloads":              p.Downloads,
		"icon_url":               p.IconURL,
		"license":                p.License,
		"status":                 p.Status,
		"published":              p.Published,
		"publication":            marketplace.PublishedLabel(p.Published, p.Status),
		"compatibility":          string(p.Compat),
		"compatibility_reason":   p.CompatReason,
		"compatibility_verified": false,
	}
	if p.Author != "" {
		d["author"] = p.Author
	}
	if len(p.Loaders) > 0 {
		d["loaders"] = p.Loaders
	}
	if len(p.GameVersions) > 0 {
		d["game_versions"] = p.GameVersions
	}
	if len(p.Categories) > 0 {
		d["categories"] = p.Categories
	}
	return d
}

// fileData serializa un artefacto. "verifiable" dice si hay un checksum fuerte
// publicado; nunca se finge una verificación que no se pueda hacer.
func fileData(f marketplace.File) map[string]any {
	d := map[string]any{
		"name":       f.Name,
		"size_bytes": f.Size,
		"primary":    f.Primary,
		"external":   f.External,
		"verifiable": f.Verifiable(),
	}
	if h, ok := f.StrongestHash(); ok {
		d["hash"] = map[string]any{"algorithm": h.Algorithm, "value": h.Value}
	}
	if !f.Verifiable() {
		d["note"] = "el proveedor no publica un checksum utilizable (sha256/sha512): sin --allow-unverified no se instala"
	}
	return d
}

// dependencyData serializa una dependencia declarada.
func dependencyData(dep marketplace.Dependency) map[string]any {
	return map[string]any{
		"project_id": dep.ProjectID,
		"version_id": dep.VersionID,
		"name":       dep.Name,
		"kind":       string(dep.Kind),
	}
}

// dependencyStatusData serializa una dependencia con su estado respecto de lo
// instalado.
func dependencyStatusData(st resolve.DependencyStatus) map[string]any {
	d := dependencyData(st.Dependency)
	d["status"] = st.Status
	d["detail"] = st.Detail
	return d
}

// dependenciesData serializa la lista de estados de dependencias.
func dependenciesData(list []resolve.DependencyStatus) []map[string]any {
	if len(list) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, st := range list {
		out = append(out, dependencyStatusData(st))
	}
	return out
}

// versionData serializa una version y su compatibilidad DECLARADA.
func versionData(v marketplace.Version, env marketplace.Environment) map[string]any {
	compat, reason := v.Compatibility(env)
	d := map[string]any{
		"source":                 string(v.Source),
		"id":                     v.ID,
		"number":                 v.Number,
		"name":                   v.Name,
		"channel":                v.Channel,
		"prerelease":             v.Prerelease(),
		"status":                 v.Status,
		"published":              v.Published,
		"publication":            marketplace.PublishedLabel(v.Published, v.Status),
		"downloads":              v.Downloads,
		"date_published":         v.DatePublished,
		"compatibility":          string(compat),
		"compatibility_reason":   reason,
		"compatibility_verified": false,
	}
	if len(v.Loaders) > 0 {
		d["loaders"] = v.Loaders
	}
	if len(v.GameVersions) > 0 {
		d["game_versions"] = v.GameVersions
	}
	d["file_count"] = len(v.Files)
	if f, ok := v.PrimaryFile(); ok {
		d["file"] = fileData(f)
	}
	if len(v.Dependencies) > 0 {
		deps := make([]map[string]any, 0, len(v.Dependencies))
		for _, dep := range v.Dependencies {
			deps = append(deps, dependencyData(dep))
		}
		d["dependencies"] = deps
	}
	return d
}

// hashData serializa un checksum para el resultado.
func hashData(h marketplace.Hash) map[string]any {
	return map[string]any{"algorithm": h.Algorithm, "value": h.Value}
} // --- `plugin sources` --------------------------------------------------------

// sourceKinds son los tipos de proyecto que publica cada marketplace. Es
// informativo: no sustituye a la comprobación por proyecto.
func sourceKinds(id marketplace.SourceID) []string {
	var kinds []marketplace.Kind
	switch id {
	case marketplace.SourceModrinth:
		kinds = []marketplace.Kind{
			marketplace.KindPlugin, marketplace.KindMod, marketplace.KindModpack,
			marketplace.KindDatapack, marketplace.KindResourcepack, marketplace.KindShader,
		}
	case marketplace.SourceHangar:
		kinds = []marketplace.Kind{marketplace.KindPlugin}
	}
	out := make([]string, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, string(k))
	}
	return out
}

// sourceEndpoint es la API pública de cada marketplace.
func sourceEndpoint(id marketplace.SourceID) string {
	switch id {
	case marketplace.SourceModrinth:
		return modrinth.DefaultBaseURL
	case marketplace.SourceHangar:
		return hangar.DefaultBaseURL
	default:
		return ""
	}
}

// sourceStrongestHash es el checksum más fuerte que publica cada marketplace.
func sourceStrongestHash(id marketplace.SourceID) string {
	switch id {
	case marketplace.SourceModrinth:
		return marketplace.HashSHA512
	case marketplace.SourceHangar:
		return marketplace.HashSHA256
	default:
		return ""
	}
}

func newPluginSourcesCmd(e *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "sources",
		Short: "Lista los marketplaces disponibles y su estado",
		Long: "Lista los marketplaces registrados en el catálogo. El estado es el de\n" +
			"REGISTRO: esta orden no consulta la red, así que no afirma que el proveedor\n" +
			"esté disponible ahora mismo (para eso, usa 'plugin search').",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cat, err := e.marketCatalog()
			if err != nil {
				return e.finish(result.FromError("plugin sources", err), "")
			}
			list := make([]map[string]any, 0, len(cat.IDs()))
			human := "Marketplaces disponibles:"
			for _, id := range cat.IDs() {
				src, ok := cat.Get(id)
				if !ok {
					continue
				}
				entry := map[string]any{
					"id":                 string(id),
					"label":              src.Label(),
					"status":             "registrada",
					"live_checked":       false,
					"verified":           false,
					"endpoint":           sourceEndpoint(id),
					"kinds":              sourceKinds(id),
					"strongest_checksum": sourceStrongestHash(id),
				}
				list = append(list, entry)
				human += fmt.Sprintf("\n  %-9s %s · tipos: %s · checksum %s",
					id, src.Label(), strings.Join(sourceKinds(id), "/"), sourceStrongestHash(id))
			}
			human += "\n(estado de registro: no se ha consultado la red)"
			data := map[string]any{
				"count":   len(list),
				"sources": list,
				"note":    "un marketplace registrado no garantiza disponibilidad: la comprobación real ocurre al consultar",
			}
			return e.finish(result.OK("plugin sources",
				fmt.Sprintf("%d marketplace(s) registrado(s)", len(list)), data), human)
		},
	}
}

// --- `plugin search` ---------------------------------------------------------

// parseKindFlag valida el tipo de proyecto pedido con --kind.
func parseKindFlag(raw string) (marketplace.Kind, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "", nil
	}
	switch marketplace.Kind(s) {
	case marketplace.KindPlugin, marketplace.KindMod, marketplace.KindModpack,
		marketplace.KindDatapack, marketplace.KindResourcepack, marketplace.KindShader:
		return marketplace.Kind(s), nil
	default:
		return "", errx.New(errx.CodeUsage, "tipo de proyecto desconocido: "+raw,
			"usa plugin, mod, modpack, datapack, resourcepack o shader").
			WithDetail("reason", "tipo_invalido")
	}
}

// searchNote es el aviso que acompaña SIEMPRE a un resultado de búsqueda.
const searchNote = "un resultado de búsqueda no demuestra compatibilidad: no se afirma que el proyecto funcione en la instancia; compruébalo con 'plugin versions <proyecto>'"

func newPluginSearchCmd(e *Env) *cobra.Command {
	var (
		sourceFlag string
		kindFlag   string
		limit      int
		offset     int
	)
	cmd := &cobra.Command{
		Use:   "search <texto>",
		Short: "Busca proyectos en los marketplaces (un resultado NO demuestra compatibilidad)",
		Long: "Busca proyectos por texto. Con -i <instancia> se calcula además la\n" +
			"compatibilidad DECLARADA por el proveedor frente a ese motor y esa versión de\n" +
			"Minecraft, y se pide al proveedor que filtre (ayuda de relevancia, no prueba).\n\n" +
			"Un resultado de búsqueda nunca se presenta como compatible verificado.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text := strings.TrimSpace(args[0])
			if text == "" {
				return e.finish(result.FromError("plugin search",
					errx.Usage("falta el texto a buscar", "indica qué proyecto buscas")), "")
			}
			if limit < 0 {
				return e.finish(result.FromError("plugin search",
					errx.Usage("el límite no puede ser negativo", "usa un número entero mayor o igual que cero")), "")
			}
			if offset < 0 {
				return e.finish(result.FromError("plugin search",
					errx.Usage("el desplazamiento no puede ser negativo", "usa un número entero mayor o igual que cero")), "")
			}
			kind, err := parseKindFlag(kindFlag)
			if err != nil {
				return e.finish(result.FromError("plugin search", err), "")
			}
			cat, err := e.marketCatalog()
			if err != nil {
				return e.finish(result.FromError("plugin search", err), "")
			}
			sources, err := selectMarketSources(cat, sourceFlag)
			if err != nil {
				return e.finish(result.FromError("plugin search", err), "")
			}
			spec, env, known, err := e.marketEnvironment()
			if err != nil {
				return e.finish(result.FromError("plugin search", err), "")
			}

			query := marketplace.SearchQuery{
				Text:      text,
				Kind:      kind,
				Env:       env,
				FilterEnv: known,
				Limit:     limit,
				Offset:    offset,
			}

			results := make([]map[string]any, 0)
			failures := make([]map[string]any, 0)
			var firstErr error
			total := 0
			queried := make([]string, 0, len(sources))
			for _, src := range sources {
				queried = append(queried, string(src.ID()))
				res, serr := src.Search(cmd.Context(), query)
				if serr != nil {
					if firstErr == nil {
						firstErr = serr
					}
					entry := map[string]any{"source": string(src.ID()), "message": errorMessage(serr)}
					if d := errx.Details(serr); d != nil {
						if r, ok := d["reason"].(string); ok {
							entry["reason"] = r
						}
					}
					failures = append(failures, entry)
					continue
				}
				total += res.Total
				for _, p := range res.Projects {
					item := projectData(p)
					item["total_matches"] = res.Total
					results = append(results, item)
				}
			}

			data := map[string]any{
				"query":           text,
				"results":         results,
				"count":           len(results),
				"total":           total,
				"environment":     environmentData(env, known),
				"filter_env":      known,
				"sources_queried": queried,
				"note":            searchNote,
			}
			if spec != nil {
				data["instance"] = spec.Slug
			}
			if kind != "" {
				data["kind"] = string(kind)
			}
			if len(failures) > 0 {
				data["failures"] = failures
			}

			human := humanSearch(text, results, failures)
			switch {
			case len(failures) == len(sources):
				// Ningún proveedor respondió: es un error, no una búsqueda vacía.
				res := result.FromError("plugin search", firstErr)
				res.Data = data
				return e.finish(res, human)
			case len(failures) > 0:
				return e.finish(result.Warn("plugin search",
					"algunas fuentes no respondieron: "+strings.Join(failureLabels(failures), ", "), data), human)
			default:
				return e.finish(result.OK("plugin search",
					searchMessage(text, len(results)), data), human)
			}
		},
	}
	cmd.Flags().StringVar(&sourceFlag, "source", "", "marketplace a consultar (por defecto, todos los registrados)")
	cmd.Flags().StringVar(&kindFlag, "kind", "", "tipo de proyecto: plugin|mod|modpack|datapack|resourcepack|shader")
	cmd.Flags().IntVar(&limit, "limit", 0, "máximo de resultados por marketplace (0 = el valor del adaptador)")
	cmd.Flags().IntVar(&offset, "offset", 0, "desplazamiento de la paginación")
	return cmd
}

// errorMessage extrae el mensaje legible de un error.
func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	_, msg, _ := errx.From(err)
	return msg
}

// failureLabels devuelve las fuentes que fallaron, para los mensajes.
func failureLabels(failures []map[string]any) []string {
	out := make([]string, 0, len(failures))
	for _, f := range failures {
		if s, ok := f["source"].(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// searchMessage describe el resultado de una búsqueda.
func searchMessage(text string, count int) string {
	if count == 0 {
		return fmt.Sprintf("sin resultados para %q", text)
	}
	return fmt.Sprintf("%d resultado(s) para %q", count, text)
}

// humanSearch compone el texto legible de una búsqueda.
func humanSearch(text string, results []map[string]any, failures []map[string]any) string {
	human := searchMessage(text, len(results))
	for _, r := range results {
		human += fmt.Sprintf("\n  %-9v %v — %v [%v] · compatibilidad: %v",
			r["source"], r["slug"], r["title"], r["kind"], r["compatibility"])
	}
	for _, f := range failures {
		human += fmt.Sprintf("\n  %-9v ✖ %v", f["source"], f["message"])
	}
	human += "\n" + searchNote
	return human
}

// --- `plugin info` -----------------------------------------------------------

func newPluginInfoCmd(e *Env) *cobra.Command {
	var sourceFlag string
	cmd := &cobra.Command{
		Use:   "info <proyecto>",
		Short: "Muestra el detalle de un proyecto del marketplace",
		Long: "Muestra lo que el proveedor DECLARA del proyecto (título, tipo, licencia,\n" +
			"loaders, versiones de Minecraft y estado de publicación) y la compatibilidad\n" +
			"declarada frente a la instancia de -i, si se indicó.\n\n" +
			"No comprueba ninguna versión ni ningún archivo: eso lo hace 'plugin install'.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := strings.TrimSpace(args[0])
			if ref == "" {
				return e.finish(result.FromError("plugin info",
					errx.Usage("falta el proyecto", "indica el slug o el identificador del proyecto")), "")
			}
			cat, err := e.marketCatalog()
			if err != nil {
				return e.finish(result.FromError("plugin info", err), "")
			}
			src, err := pickMarketSource(cat, sourceFlag)
			if err != nil {
				return e.finish(result.FromError("plugin info", err), "")
			}
			spec, env, known, err := e.marketEnvironment()
			if err != nil {
				return e.finish(result.FromError("plugin info", err), "")
			}
			proj, err := src.Project(cmd.Context(), ref)
			if err != nil {
				return e.finish(result.FromError("plugin info", err), "")
			}
			proj.Compat, proj.CompatReason = proj.Compatibility(env)

			data := projectData(*proj)
			data["project"] = map[string]any{
				"id": proj.ID, "slug": proj.Slug, "title": proj.Title,
				"kind": string(proj.Kind), "url": proj.URL,
			}
			data["environment"] = environmentData(env, known)
			if spec != nil {
				data["instance"] = spec.Slug
			}
			data["note"] = "la compatibilidad es la DECLARADA para el proyecto completo; solo 'plugin install' resuelve una versión y un archivo exactos"

			human := fmt.Sprintf("%s (%s:%s) — %s", proj.Title, proj.Source, proj.Slug, proj.Kind)
			human += "\n  " + marketplace.PublishedLabel(proj.Published, proj.Status)
			if len(proj.Loaders) > 0 {
				human += " · loaders declarados: " + strings.Join(proj.Loaders, ", ")
			}
			if len(proj.GameVersions) > 0 {
				human += " · Minecraft: " + strings.Join(proj.GameVersions, ", ")
			}
			human += "\n  compatibilidad declarada: " + string(proj.Compat)
			if proj.CompatReason != "" {
				human += " (" + proj.CompatReason + ")"
			}
			if proj.URL != "" {
				human += "\n  " + proj.URL
			}
			return e.finish(result.OK("plugin info",
				fmt.Sprintf("%s en %s", proj.Title, proj.Source), data), human)
		},
	}
	cmd.Flags().StringVar(&sourceFlag, "source", "", "marketplace a consultar (por defecto, el primero registrado)")
	return cmd
}

// --- `plugin versions` -------------------------------------------------------

func newPluginVersionsCmd(e *Env) *cobra.Command {
	var (
		sourceFlag string
		compatible bool
	)
	cmd := &cobra.Command{
		Use:   "versions <proyecto>",
		Short: "Lista las versiones de un proyecto (filtradas por el entorno de la instancia)",
		Long: "Lista las versiones publicadas de un proyecto. Con -i <instancia> se piden\n" +
			"solo las que declaran ese loader y esa versión de Minecraft, y con\n" +
			"--compatible se descartan las que no coinciden.\n\n" +
			"La compatibilidad se calcula sobre lo que DECLARA el proveedor, no sobre el\n" +
			"contenido del artefacto.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := strings.TrimSpace(args[0])
			if ref == "" {
				return e.finish(result.FromError("plugin versions",
					errx.Usage("falta el proyecto", "indica el slug o el identificador del proyecto")), "")
			}
			spec, env, known, err := e.marketEnvironment()
			if err != nil {
				return e.finish(result.FromError("plugin versions", err), "")
			}
			if compatible && !known {
				return e.finish(result.FromError("plugin versions",
					errx.Usage("--compatible necesita una instancia",
						"indica la instancia con -i <slug>: sin ella no hay entorno con el que comparar").
						WithDetail("reason", resolve.ReasonEntorno)), "")
			}
			cat, err := e.marketCatalog()
			if err != nil {
				return e.finish(result.FromError("plugin versions", err), "")
			}
			src, err := pickMarketSource(cat, sourceFlag)
			if err != nil {
				return e.finish(result.FromError("plugin versions", err), "")
			}
			proj, err := src.Project(cmd.Context(), ref)
			if err != nil {
				return e.finish(result.FromError("plugin versions", err), "")
			}
			var filter marketplace.VersionFilter
			if known {
				filter.Loaders = []string{env.LoaderName()}
				filter.GameVersions = []string{env.GameVersion}
			}
			versions, err := src.Versions(cmd.Context(), projectRef(proj), filter)
			if err != nil {
				return e.finish(result.FromError("plugin versions", err), "")
			}

			out := make([]map[string]any, 0, len(versions))
			for _, v := range versions {
				if compatible {
					if c, _ := v.Compatibility(env); c != marketplace.Compatible {
						continue
					}
				}
				out = append(out, versionData(v, env))
			}

			data := map[string]any{
				"source":          string(src.ID()),
				"project":         map[string]any{"id": proj.ID, "slug": proj.Slug, "title": proj.Title, "kind": string(proj.Kind), "url": proj.URL},
				"versions":        out,
				"count":           len(out),
				"total":           len(versions),
				"environment":     environmentData(env, known),
				"compatible_only": compatible,
				"note":            "la compatibilidad es la DECLARADA por el proveedor para cada versión; el archivo exacto se resuelve al instalar",
			}
			if spec != nil {
				data["instance"] = spec.Slug
			}

			human := humanVersions(proj.Title, string(src.ID()), out, known)
			if len(out) == 0 {
				return e.finish(result.OK("plugin versions",
					versionsEmptyMessage(proj.Title, env, known, compatible), data), human)
			}
			return e.finish(result.OK("plugin versions",
				fmt.Sprintf("%d versión(es) de %s", len(out), proj.Title), data), human)
		},
	}
	cmd.Flags().StringVar(&sourceFlag, "source", "", "marketplace a consultar (por defecto, el primero registrado)")
	cmd.Flags().BoolVar(&compatible, "compatible", false, "mostrar solo las versiones compatibles con la instancia de -i")
	return cmd
}

// projectRef elige con qué referencia identificar el proyecto al listar
// versiones. Se prefiere el slug: la API de versiones de Hangar identifica el
// proyecto por slug, y la de Modrinth acepta ambos.
func projectRef(p *marketplace.Project) string {
	if s := strings.TrimSpace(p.Slug); s != "" {
		return s
	}
	return strings.TrimSpace(p.ID)
}

// versionsEmptyMessage explica por qué no hay versiones.
func versionsEmptyMessage(title string, env marketplace.Environment, known, compatible bool) string {
	switch {
	case compatible && known:
		return fmt.Sprintf("%s no tiene ninguna versión compatible con %s + Minecraft %s",
			title, env.LoaderName(), env.GameVersion)
	case known:
		return fmt.Sprintf("%s no publica versiones para %s + Minecraft %s",
			title, env.LoaderName(), env.GameVersion)
	default:
		return fmt.Sprintf("%s no publica ninguna versión", title)
	}
}

// humanVersions compone el texto legible del listado de versiones.
func humanVersions(title, source string, versions []map[string]any, known bool) string {
	human := fmt.Sprintf("Versiones de %s (%s):", title, source)
	if len(versions) == 0 {
		human += "\n  (ninguna)"
	}
	for _, v := range versions {
		human += fmt.Sprintf("\n  %-14v %-9v %v · %v", v["number"], v["channel"], v["compatibility"], v["publication"])
	}
	if !known {
		human += "\n(sin instancia: la compatibilidad queda como desconocida)"
	}
	return human
}

// --- `plugin install` --------------------------------------------------------

// buildMarketDownloadPolicy traduce los flags de red y límites a la política de
// 11.1. Es EXACTAMENTE la misma que usa `plugin add`.
func buildMarketDownloadPolicy(maxSizeFlag, conflictFlag string, allowHTTP, allowPrivate, allowLoopback bool) (download.Policy, error) {
	opts, err := pluginInstallOptions("", maxSizeFlag, "", conflictFlag, allowHTTP, allowPrivate, allowLoopback)
	if err != nil {
		return download.Policy{}, err
	}
	return opts.Download, nil
}

// splitProjectVersion separa "proyecto@versión" en sus dos partes.
func splitProjectVersion(raw string) (project, version string) {
	s := strings.TrimSpace(raw)
	if i := strings.LastIndex(s, "@"); i > 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
	}
	return s, ""
}

// resolutionReasons son los motivos que significan "la resolución no está
// completa" (AC-06): los de resolve.Problems más la defensa en profundidad del
// instalador. Con cualquiera de ellos, data.resolution_complete es false.
var resolutionReasons = map[string]bool{
	resolve.ReasonEntorno:             true,
	resolve.ReasonNoPublicado:         true,
	resolve.ReasonTipo:                true,
	resolve.ReasonSinVersion:          true,
	resolve.ReasonVersionInexistente:  true,
	resolve.ReasonVersionIncompatible: true,
	resolve.ReasonSinArchivo:          true,
	resolve.ReasonArchivoNoInstalable: true,
	resolve.ReasonChecksum:            true,
	resolve.ReasonDependencia:         true,
	// install.ReasonFuente/ReasonRegistro no son de resolución; el motivo de
	// defensa en profundidad del instalador sí:
	"resolucion_incompleta": true,
}

// resolutionProblems traduce el error tipado a la lista de problemas de la
// resolución, con los motivos estables de resolve.
func resolutionProblems(err error) []map[string]any {
	d := errx.Details(err)
	if d == nil {
		return nil
	}
	reason, _ := d["reason"].(string)
	if reason == "" || !resolutionReasons[reason] {
		return nil
	}
	_, message, hint := errx.From(err)
	return []map[string]any{{
		"reason":  reason,
		"message": message,
		"hint":    hint,
		"fatal":   true,
	}}
}

// marketQueryReasons son los motivos que indican que NO se llegó a resolver
// porque falló la consulta al proveedor o el uso era inválido: la resolución no
// llegó a producirse.
var marketQueryReasons = map[string]bool{
	marketplace.ReasonNotFound:          true,
	marketplace.ReasonRed:               true,
	marketplace.ReasonUpstream:          true,
	marketplace.ReasonRespuestaInvalida: true,
	marketplace.ReasonRespuestaExcesiva: true,
	install.ReasonFuente:                true,
	"fuente_ausente":                    true,
	"proyecto_vacio":                    true,
}

// installStage clasifica un fallo de instalación en tres etapas estables:
//
//   - "resolucion": la resolución NO es completa (AC-06) ⇒ resolution_complete
//     false y motivos en "problems";
//   - "consulta": no se pudo consultar el proveedor ⇒ tampoco hay resolución;
//   - "instalacion": la resolución era completa y falló la descarga, la
//     verificación o el registro ⇒ resolution_complete true.
func installStage(err error) string {
	reason := ""
	if d := errx.Details(err); d != nil {
		reason, _ = d["reason"].(string)
	}
	switch {
	case resolutionReasons[reason]:
		return "resolucion"
	case marketQueryReasons[reason]:
		return "consulta"
	default:
		return "instalacion"
	}
}

// installFailureData compone el "data" del documento cuando la instalación
// falla: SIEMPRE deja "resolution_complete" explícito y, si el fallo es de
// resolución, los motivos estables en "problems".
func installFailureData(spec *instance.Spec, source marketplace.SourceID, project, version string, err error) map[string]any {
	stage := installStage(err)
	data := map[string]any{
		"source":              string(source),
		"project":             project,
		"version_requested":   version,
		"stage":               stage,
		"resolution_complete": stage == "instalacion",
	}
	if spec != nil {
		data["instance"] = spec.Slug
	}
	if problems := resolutionProblems(err); len(problems) > 0 {
		data["problems"] = problems
	}
	if d := errx.Details(err); d != nil {
		if reason, ok := d["reason"].(string); ok && reason != "" {
			data["reason"] = reason
		}
	}
	return data
}

func newPluginInstallCmd(e *Env) *cobra.Command {
	var (
		sourceFlag       string
		channels         []string
		allowPrerelease  bool
		allowUnverified  bool
		allowMissingDeps bool
		conflictFlag     string
		maxSizeFlag      string
		allowHTTP        bool
		allowPrivate     bool
		allowLoopback    bool
	)
	cmd := &cobra.Command{
		Use:   "install <proyecto>[@<versión>]",
		Short: "Resuelve, descarga e instala un plugin/mod desde un marketplace",
		Long: "Resuelve el proyecto a una versión y un archivo EXACTOS declarados por el\n" +
			"marketplace, descarga el artefacto con la política de seguridad de badomens\n" +
			"(límite durante la transferencia, https obligatorio, anti-SSRF, verificación\n" +
			"de checksum e inspección estructural JAR/ZIP) y solo entonces lo coloca en su\n" +
			"sitio y actualiza el registro de procedencia y la lista de la instancia.\n\n" +
			"Si la resolución no es completa (falta la versión, el loader, la versión de\n" +
			"Minecraft o un checksum exigible) NO se descarga nada y el comando falla con\n" +
			"resolution_complete:false y el motivo en error.details.reason.\n\n" +
			"Las dependencias NUNCA se instalan solas: o se exigen (error) o se aceptan\n" +
			"explícitamente con --allow-missing-deps (aviso).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, version := splitProjectVersion(args[0])
			if project == "" {
				return e.finish(result.FromError("plugin install",
					errx.Usage("falta el proyecto", "indica el slug del proyecto: 'plugin install viaversion'")), "")
			}
			spec, err := e.activeSpec()
			if err != nil {
				return e.finish(result.FromError("plugin install", err), "")
			}
			cat, err := e.marketCatalog()
			if err != nil {
				return e.finish(result.FromError("plugin install", err), "")
			}
			src, err := pickMarketSource(cat, sourceFlag)
			if err != nil {
				return e.finish(result.FromError("plugin install", err), "")
			}
			client, err := e.marketClient(cat)
			if err != nil {
				return e.finish(result.FromError("plugin install", err), "")
			}
			policy, err := buildMarketDownloadPolicy(maxSizeFlag, conflictFlag, allowHTTP, allowPrivate, allowLoopback)
			if err != nil {
				return e.finish(result.FromError("plugin install", err), "")
			}

			res, err := client.Install(cmd.Context(), install.Request{
				Spec:             spec,
				Source:           src.ID(),
				Project:          project,
				Version:          version,
				Channels:         channels,
				AllowPrerelease:  allowPrerelease,
				AllowUnverified:  allowUnverified,
				AllowMissingDeps: allowMissingDeps,
				Download:         policy,
				Archive:          archive.DefaultLimits(),
			})
			if err != nil {
				data := installFailureData(spec, src.ID(), project, version, err)
				human := "no se instaló nada: " + errorMessage(err)
				if problems, ok := data["problems"].([]map[string]any); ok {
					for _, p := range problems {
						human += fmt.Sprintf("\n  motivo %v: %v", p["reason"], p["message"])
					}
				}
				if hint := hintOf(err); hint != "" {
					human += "\n  " + hint
				}
				failure := result.FromError("plugin install", err)
				failure.Data = data
				return e.finish(failure, human)
			}

			name := res.Installed.Name
			// El registro de la instancia lo mantiene el llamador (convención de
			// 11.1) y SOLO se toca cuando la instalación ha terminado del todo: el
			// archivo ya está en su sitio y la procedencia ya la escribió el
			// instalador. Si el guardado falla, se deshace todo para no dejar
			// disco, registro y procedencia desincronizados.
			spec.Plugins = appendUnique(spec.Plugins, name)
			if serr := specSaved(e, spec); serr != nil {
				werr := errx.Wrap(errx.CodeGeneral, serr,
					"no se pudo guardar el registro de la instancia",
					"se retiró "+name+" para no dejar el registro y el disco desincronizados")
				if rerr := rollbackMarketInstall(client, spec, name); rerr != nil {
					// WithDetail encadena el detalle y devuelve el propio error: se
					// reasigna para no descartar el valor sin perder el original.
					werr = werr.WithDetail("rollback_error", rerr.Error())
				}
				return e.finish(result.FromError("plugin install", werr), "")
			}

			data := marketInstallData(spec, res)
			human := humanInstall(spec, res, client.Installer)
			if warnings, ok := data["warnings"].([]string); ok && len(warnings) > 0 {
				return e.finish(result.Warn("plugin install", human, data), human)
			}
			return e.finish(result.OK("plugin install", human, data), human)
		},
	}
	cmd.Flags().StringVar(&sourceFlag, "source", "", "marketplace a consultar (por defecto, el primero registrado)")
	cmd.Flags().StringSliceVar(&channels, "channel", nil, "canales aceptados (repetible): release, beta, alpha")
	cmd.Flags().BoolVar(&allowPrerelease, "allow-prerelease", false, "aceptar canales preliminares (beta/alpha/snapshot)")
	cmd.Flags().BoolVar(&allowUnverified, "allow-unverified", false, "aceptar un artefacto sin checksum publicado (sin verificación de integridad)")
	cmd.Flags().BoolVar(&allowMissingDeps, "allow-missing-deps", false, "aceptar dependencias obligatorias ausentes (aviso en lugar de error)")
	cmd.Flags().StringVar(&conflictFlag, "on-conflict", string(download.ConflictFail), "qué hacer si el archivo ya existe: fail|rename|replace")
	cmd.Flags().StringVar(&maxSizeFlag, "max-size", "", "límite de la descarga en bytes o con sufijo K/M/G (por defecto 64M)")
	cmd.Flags().BoolVar(&allowHTTP, "allow-http", false, "permitir descarga por http:// sin cifrado (riesgo asumido)")
	cmd.Flags().BoolVar(&allowPrivate, "allow-private", false, "permitir destinos en redes privadas/link-local/metadatos (riesgo de SSRF)")
	cmd.Flags().BoolVar(&allowLoopback, "allow-loopback", false, "permitir destinos loopback (127.0.0.1, ::1)")
	return cmd
}

// hintOf extrae la pista de un error tipado.
func hintOf(err error) string {
	_, _, hint := errx.From(err)
	return hint
}

// rollbackMarketInstall deshace una instalación cuyo registro de instancia no
// se pudo guardar: retira el archivo recién instalado y su entrada de
// procedencia, para no dejar el disco, el registro de la instancia y el de
// procedencia desincronizados.
func rollbackMarketInstall(client *install.Client, spec *instance.Spec, name string) error {
	var firstErr error
	if client.Installer != nil {
		if err := client.Installer.Remove(spec, name); err != nil && !errors.Is(err, os.ErrNotExist) {
			firstErr = err
		}
	}
	if client.Provenance != nil {
		reg, err := client.Provenance.Load(spec)
		switch {
		case err != nil:
			if firstErr == nil {
				firstErr = err
			}
		case reg.Remove(name):
			if serr := client.Provenance.Save(spec, reg); serr != nil && firstErr == nil {
				firstErr = serr
			}
		}
	}
	return firstErr
}

// marketInstallData compone el "data" de una instalación completada. Es ADITIVO
// y no rompe ninguna clave del contrato.
func marketInstallData(spec *instance.Spec, res *install.Result) map[string]any {
	r := res.Resolution
	installed := map[string]any{
		"name":              res.Installed.Name,
		"size_bytes":        res.Installed.Bytes,
		"sha256":            res.Installed.SHA256,
		"checksum_verified": res.Installed.ChecksumVerified,
		"archive":           res.Installed.Archive,
	}
	if res.Installed.RenamedFrom != "" {
		installed["renamed_from"] = res.Installed.RenamedFrom
	}
	if res.Installed.Replaced {
		installed["replaced"] = true
	}

	data := map[string]any{
		"instance": spec.Slug,
		"source":   string(r.Source),
		"project": map[string]any{
			"id": r.Project.ID, "slug": r.Project.Slug,
			"title": r.Project.Title, "kind": string(r.Project.Kind),
		},
		"version":             r.Version.Number,
		"version_id":          r.Version.ID,
		"channel":             r.Version.Channel,
		"compatibility":       string(r.Compat),
		"publication":         r.Publication,
		"resolution_complete": r.Complete,
		"file": map[string]any{
			"name":       res.Installed.Name,
			"size_bytes": res.Installed.Bytes,
			"url":        r.File.URL,
			"external":   r.File.External,
		},
		"installed":        installed,
		"provenance":       res.Record,
		"security_warning": plugins.ThirdPartyWarning(),
	}
	if h := r.Hash; h.Value != "" {
		data["hash"] = hashData(h)
	}
	if deps := dependenciesData(r.Dependencies); len(deps) > 0 {
		data["dependencies"] = deps
	}
	warnings := append([]string{}, res.Warnings...)
	if w := plugins.WarnFor([]string{res.Installed.Name}); w != "" {
		warnings = append(warnings, w)
	}
	if len(warnings) > 0 {
		data["warnings"] = warnings
	}
	return data
}

// humanInstall compone el texto legible de una instalación.
func humanInstall(spec *instance.Spec, res *install.Result, installer *plugins.Installer) string {
	r := res.Resolution
	human := fmt.Sprintf("%s instalado en %s", res.Installed.Name, spec.Slug)
	if installer != nil {
		human += " (" + installer.Dir(spec) + ")"
	}
	human += fmt.Sprintf("\n  %s:%s %s (%s) · %s + Minecraft %s · compatibilidad declarada: %s",
		r.Source, firstNonEmptyStr(r.Project.Slug, r.Project.ID), r.Version.Number,
		channelLabel(r.Version.Channel),
		firstNonEmptyStr(res.Record.Loader, spec.Engine),
		firstNonEmptyStr(res.Record.GameVersion, spec.Version),
		string(r.Compat))
	human += fmt.Sprintf("\n  %s · %d entradas en el JAR/ZIP",
		download.HumanBytes(res.Installed.Bytes), res.Installed.Archive.Entries)
	if h := r.Hash; h.Value != "" {
		human += "\n  checksum " + strings.ToUpper(h.Algorithm) + " verificado: " + h.Value
	} else {
		human += "\n  SIN verificación de integridad (el proveedor no publica checksum)"
	}
	if res.Installed.RenamedFrom != "" {
		human += "\nya existía " + res.Installed.RenamedFrom + ": se instaló como " + res.Installed.Name
	}
	for _, w := range res.Warnings {
		human += "\n⚠️ " + w
	}
	human += "\n⚠️ " + plugins.ThirdPartyWarning()
	return human
}

// channelLabel describe el canal de una versión.
func channelLabel(channel string) string {
	if c := strings.TrimSpace(channel); c != "" {
		return c
	}
	return "desconocido"
}

// firstNonEmptyStr devuelve el primer valor no vacío.
func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// --- `plugin updates` --------------------------------------------------------

// updateStatusLabel traduce el estado de la comprobación a texto legible.
func updateStatusLabel(status string) string {
	switch status {
	case install.UpdateCurrent:
		return "al día"
	case install.UpdateAvailable:
		return "actualización disponible"
	case install.UpdateBlocked:
		return "bloqueada"
	default:
		return "desconocida"
	}
}

func newPluginUpdatesCmd(e *Env) *cobra.Command {
	var (
		persistTargets  bool
		channels        []string
		allowPrerelease bool
	)
	cmd := &cobra.Command{
		Use:   "updates",
		Short: "Comprueba si hay versiones nuevas publicadas (no instala nada)",
		Long: "Comprueba, para cada plugin/mod con procedencia registrada, si el\n" +
			"marketplace publica una versión más reciente compatible con la instancia.\n\n" +
			"NO descarga ni instala nada y NO modifica el archivo instalado. Con\n" +
			"--persist-targets anota la versión OBJETIVO en el registro de procedencia\n" +
			"(es un dato, no una instalación).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := e.activeSpec()
			if err != nil {
				return e.finish(result.FromError("plugin updates", err), "")
			}
			cat, err := e.marketCatalog()
			if err != nil {
				return e.finish(result.FromError("plugin updates", err), "")
			}
			client, err := e.marketClient(cat)
			if err != nil {
				return e.finish(result.FromError("plugin updates", err), "")
			}
			checks, err := client.CheckUpdates(cmd.Context(), install.UpdateRequest{
				Spec:            spec,
				Channels:        channels,
				AllowPrerelease: allowPrerelease,
				PersistTargets:  persistTargets,
			})
			if err != nil {
				return e.finish(result.FromError("plugin updates", err), "")
			}

			available, blocked, unknown := 0, 0, 0
			for _, c := range checks {
				switch c.Status {
				case install.UpdateAvailable:
					available++
				case install.UpdateBlocked:
					blocked++
				case install.UpdateUnknown:
					unknown++
				}
			}
			data := map[string]any{
				"instance":           spec.Slug,
				"updates":            checks,
				"checked":            len(checks),
				"available":          available,
				"blocked":            blocked,
				"unknown":            unknown,
				"persisted_targets":  persistTargets,
				"installed_modified": false,
				"note":               "comprobar actualizaciones no descarga ni modifica nada; instala explícitamente con 'plugin install <proyecto>@<versión>'",
			}
			human := humanUpdates(spec.Slug, checks, persistTargets)

			if available > 0 || blocked > 0 {
				msg := fmt.Sprintf("%d actualización(es) disponible(s) de %d comprobado(s)", available, len(checks))
				if blocked > 0 {
					msg += fmt.Sprintf("; %d comprobación(es) bloqueada(s)", blocked)
				}
				return e.finish(result.Warn("plugin updates", msg, data), human)
			}
			return e.finish(result.OK("plugin updates",
				fmt.Sprintf("no hay actualizaciones de %d comprobado(s)", len(checks)), data), human)
		},
	}
	cmd.Flags().BoolVar(&persistTargets, "persist-targets", false, "anotar la versión objetivo detectada en el registro (no instala nada)")
	cmd.Flags().StringSliceVar(&channels, "channel", nil, "canales aceptados (repetible): release, beta, alpha")
	cmd.Flags().BoolVar(&allowPrerelease, "allow-prerelease", false, "aceptar canales preliminares al buscar la versión nueva")
	return cmd
}

// humanUpdates compone el texto legible de la comprobación de actualizaciones.
func humanUpdates(slug string, checks []install.UpdateCheck, persist bool) string {
	human := fmt.Sprintf("Actualizaciones de %s:", slug)
	if len(checks) == 0 {
		human += "\n  (no hay plugins/mods instalados)"
	}
	for _, c := range checks {
		line := fmt.Sprintf("\n  %-40s %s", c.Name, updateStatusLabel(c.Status))
		if c.Version != "" {
			line += " · instalada " + c.Version
		}
		if c.TargetVersion != "" && c.Status != install.UpdateCurrent {
			line += " → " + c.TargetVersion
		}
		human += line
		if c.Detail != "" {
			human += "\n      " + c.Detail
		}
	}
	human += "\n(comprobar actualizaciones no instala nada"
	if persist {
		human += "; se anotó la versión objetivo en el registro"
	}
	human += ")"
	return human
}
