// Package resolve implementa el RESOLVEDOR de marketplaces: decide qué versión y
// qué archivo exactos se van a instalar, y bajo qué condiciones. No descarga, no
// escribe y no registra nada (eso es del paquete install).
//
// Regla de AC-06: una resolución incompleta NO es un éxito. Si falta la versión
// exacta, el loader, la versión de Minecraft o el checksum, Resolve devuelve un
// error tipado con la explicación y jamás "lo que haya".
package resolve

import (
	"context"
	"sort"
	"strings"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/plugins"
	"github.com/MrZudro/badomens/internal/plugins/marketplace"
)

// Motivos estables de resolución incompleta (error.details.reason).
const (
	// ReasonEntorno: falta el loader o la versión de Minecraft de la instancia.
	ReasonEntorno = "entorno_incompleto"
	// ReasonNoPublicado: el proyecto no está publicado.
	ReasonNoPublicado = "proyecto_no_publicado"
	// ReasonTipo: el tipo de proyecto no se puede instalar en ese motor.
	ReasonTipo = "tipo_no_instalable"
	// ReasonSinVersion: ninguna versión declara ese loader y esa versión de MC.
	ReasonSinVersion = "sin_version_compatible"
	// ReasonVersionInexistente: la versión pedida no existe en el proyecto.
	ReasonVersionInexistente = "version_inexistente"
	// ReasonVersionIncompatible: la versión pedida existe pero no es compatible.
	ReasonVersionIncompatible = "version_incompatible"
	// ReasonSinArchivo: la versión no publica ningún artefacto descargable.
	ReasonSinArchivo = "sin_archivo"
	// ReasonArchivoNoInstalable: el artefacto no es un .jar/.zip.
	ReasonArchivoNoInstalable = "archivo_no_instalable"
	// ReasonChecksum: la versión no publica un checksum fuerte utilizable.
	ReasonChecksum = "checksum_ausente"
	// ReasonDependencia: falta una dependencia obligatoria (o hay una declarada
	// como incompatible instalada).
	ReasonDependencia = "dependencia_no_satisfecha"
)

// Estados de una dependencia respecto de lo instalado.
const (
	// DepSatisfecha: la dependencia está instalada (o no hace falta).
	DepSatisfecha = "satisfecha"
	// DepAusente: dependencia obligatoria que no está instalada.
	DepAusente = "ausente"
	// DepIncompatible: el proveedor la declara incompatible y está instalada.
	DepIncompatible = "incompatible"
	// DepOpcional: dependencia opcional; no condiciona la instalación.
	DepOpcional = "opcional"
	// DepIncluida: la dependencia va incluida en el propio artefacto.
	DepIncluida = "incluida"
	// DepDesconocida: no se puede determinar.
	DepDesconocida = "desconocida"
	// DepNoVerificable: el proveedor la declara obligatoria pero no la
	// identifica, así que no se puede comprobar si está instalada.
	DepNoVerificable = "no_verificable"
)

// ChannelsDefault son los canales aceptados si no se piden otros: solo los
// lanzamientos estables.
var ChannelsDefault = []string{"release"}

// Request describe qué se quiere resolver.
type Request struct {
	// Source es el marketplace consultado.
	Source marketplace.Source
	// Project es el slug o el identificador del proyecto.
	Project string
	// Env describe la instancia destino (loader y versión de Minecraft).
	Env marketplace.Environment
	// Version fija la versión exacta (número o identificador). Vacío ⇒ la más
	// reciente que cumpla el entorno.
	Version string
	// Channels son los canales aceptados (release, beta, alpha...). Vacío ⇒
	// ChannelsDefault.
	Channels []string
	// AllowPrerelease acepta canales que no sean de lanzamiento estable.
	AllowPrerelease bool
	// AllowUnverified acepta un artefacto sin checksum publicado. Por defecto la
	// resolución FALLA: sin checksum no hay verificación de integridad posible.
	AllowUnverified bool
	// AllowMissingDeps acepta dependencias obligatorias ausentes (queda como
	// aviso en lugar de error). Nunca se instalan solas.
	AllowMissingDeps bool
	// Installed es el registro de lo ya instalado, con el que se evalúan las
	// dependencias declaradas.
	Installed []plugins.Record
	// VersionLimit acota cuántas versiones se piden al proveedor.
	VersionLimit int
}

// DependencyStatus es el estado de una dependencia declarada.
type DependencyStatus struct {
	// Dependency es la dependencia tal y como la declara el proveedor.
	Dependency marketplace.Dependency `json:"dependency"`
	// Status es uno de los valores Dep*.
	Status string `json:"status"`
	// Detail explica el estado.
	Detail string `json:"detail,omitempty"`
}

// Resolution es el resultado de resolver un artefacto.
type Resolution struct {
	// Source identifica el marketplace.
	Source marketplace.SourceID `json:"source"`
	// Project es el proyecto resuelto.
	Project marketplace.Project `json:"project"`
	// Version es la versión exacta resuelta.
	Version marketplace.Version `json:"version"`
	// File es el artefacto exacto que se descargará.
	File marketplace.File `json:"file"`
	// Hash es el checksum con el que se verificará (puede estar vacío si el
	// proveedor no publica ninguno y el llamador lo ha aceptado).
	Hash marketplace.Hash `json:"hash,omitempty"`
	// Compat es la compatibilidad declarada frente al entorno.
	Compat marketplace.Compatibility `json:"compatibility"`
	// Publication es la etiqueta legible del estado de publicación.
	Publication string `json:"publication"`
	// Complete indica si la resolución es utilizable. Si es false, Resolve
	// devuelve además un error tipado.
	Complete bool `json:"complete"`
	// Problems son los motivos que impiden completar la resolución.
	Problems []Problem `json:"problems,omitempty"`
	// Warnings son avisos que no impiden la instalación.
	Warnings []string `json:"warnings,omitempty"`
	// Dependencies es el estado de las dependencias declaradas.
	Dependencies []DependencyStatus `json:"dependencies,omitempty"`
}

// Problem es un motivo de resolución incompleta.
type Problem struct {
	// Reason es el código estable del motivo.
	Reason string `json:"reason"`
	// Message explica el problema.
	Message string `json:"message"`
	// Hint sugiere qué hacer.
	Hint string `json:"hint,omitempty"`
	// Fatal indica si impide completar la resolución.
	Fatal bool `json:"fatal"`
}

// Resolve consulta el marketplace y devuelve la versión y el archivo exactos.
//
// Si la resolución no es completa devuelve una Resolution con el detalle (para
// poder informar al usuario) Y un error tipado: nunca hay descarga "de lo que
// haya".
func Resolve(ctx context.Context, req Request) (*Resolution, error) {
	res := &Resolution{Complete: true}

	if req.Source == nil {
		return nil, errx.New(errx.CodeUsage, "no se indicó el marketplace",
			"usa uno de los marketplaces registrados").WithDetail("reason", "fuente_ausente")
	}
	project := strings.TrimSpace(req.Project)
	if project == "" {
		return nil, errx.New(errx.CodeUsage, "falta el proyecto a resolver",
			"indica el slug o el identificador del proyecto").WithDetail("reason", "proyecto_vacio")
	}
	res.Source = req.Source.ID()

	// 1. El entorno es imprescindible: sin loader y sin versión de Minecraft no
	//    se puede afirmar compatibilidad, y esto no es una resolución completa.
	loader := req.Env.LoaderName()
	if loader == "" {
		return fail(res, ReasonEntorno, errx.CodeUsage,
			"no se pudo determinar el loader del servidor",
			"indica el motor o el loader (paper, fabric, forge, neoforge)")
	}
	mc := strings.TrimSpace(req.Env.GameVersion)
	if mc == "" {
		return fail(res, ReasonEntorno, errx.CodeUsage,
			"no se pudo determinar la versión de Minecraft del servidor",
			"indica la versión de Minecraft de la instancia")
	}

	// 2. Proyecto: tipo, publicación y compatibilidad declarada.
	proj, err := req.Source.Project(ctx, project)
	if err != nil {
		return nil, err
	}
	res.Project = *proj
	res.Publication = marketplace.PublishedLabel(proj.Published, proj.Status)

	if !proj.Published {
		return fail(res, ReasonNoPublicado, errx.CodeUsage,
			"el proyecto "+displayName(*proj)+" no está publicado en "+string(res.Source)+
				" (estado: "+statusText(proj.Status)+")",
			"elige un proyecto publicado")
	}
	if engine := engineName(req.Env); engine != "" && !proj.Kind.Accepts(engine) {
		return fail(res, ReasonTipo, errx.CodeUsage,
			"un proyecto de tipo "+string(proj.Kind)+" no se puede instalar en un servidor "+engine,
			"instala plugin en paper y mod en fabric, forge o neoforge")
	}

	// 3. Versiones que declaran ese loader y esa versión de Minecraft.
	versions, err := req.Source.Versions(ctx, proj.ID, marketplace.VersionFilter{
		Loaders:      []string{loader},
		GameVersions: []string{mc},
		Limit:        req.VersionLimit,
	})
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return fail(res, ReasonSinVersion, errx.CodeUsage,
			"el proyecto "+displayName(*proj)+" no publica ninguna versión para "+loader+" y Minecraft "+mc,
			"comprueba el motor y la versión de Minecraft de la instancia, o usa otra fuente")
	}
	sortVersions(versions)

	// 4. Selección de la versión exacta.
	selected, err := selectVersion(res, versions, loader, mc, req)
	if err != nil {
		return nil, err
	}
	res.Version = *selected
	res.Compat, _ = selected.Compatibility(req.Env)
	res.Publication = marketplace.PublishedLabel(selected.Published, selected.Status)
	if res.Compat != marketplace.Compatible {
		return fail(res, ReasonVersionIncompatible, errx.CodeUsage,
			"la versión "+selected.Number+" no es compatible con "+loader+" y Minecraft "+mc,
			"elige otra versión o revisa el entorno de la instancia")
	}

	// 5. Archivo exacto.
	file, ok := selected.PrimaryFile()
	if !ok {
		return fail(res, ReasonSinArchivo, errx.CodeUsage,
			"la versión "+selected.Number+" no publica ningún archivo descargable",
			"elige otra versión del proyecto")
	}
	if !installableName(file.Name) {
		return fail(res, ReasonArchivoNoInstalable, errx.CodeUsage,
			"el archivo "+file.Name+" no es un .jar ni un .zip instalable",
			"elige otra versión del proyecto")
	}
	res.File = file

	// 6. Integridad: sin checksum fuerte no hay verificación posible. No se
	//    finge: o se falla, o se acepta explícitamente y queda como aviso.
	if hash, hasHash := file.StrongestHash(); hasHash {
		res.Hash = hash
	} else if req.AllowUnverified {
		res.Warnings = append(res.Warnings,
			"el proveedor no publica un checksum utilizable (sha256/sha512) para "+file.Name+
				": se instalará SIN verificación de integridad")
	} else {
		return fail(res, ReasonChecksum, errx.CodeUsage,
			"el proveedor no publica un checksum utilizable (sha256/sha512) para "+file.Name,
			"sin checksum no se puede verificar la integridad; acepta el riesgo de forma explícita o usa otro origen")
	}

	// 7. Dependencias declaradas: se resuelven explícitamente contra lo instalado.
	res.Dependencies = evaluateDependencies(*selected, req.Installed)
	if problem := dependencyProblem(*selected, res.Dependencies, req.AllowMissingDeps, res); problem != nil {
		return fail(res, ReasonDependencia, errx.CodeUsage, problem.Message, problem.Hint)
	}

	res.Complete = true
	return res, nil
}

// selectVersion elige la versión exacta. Con Request.Version fija se exige esa
// versión; sin ella se toma la más reciente que declare el entorno, esté
// publicada y su canal esté aceptado.
func selectVersion(res *Resolution, versions []marketplace.Version, loader, mc string, req Request) (*marketplace.Version, error) {
	want := strings.TrimSpace(req.Version)
	if want != "" {
		for i := range versions {
			v := versions[i]
			if !sameVersion(v, want) {
				continue
			}
			if !v.Published {
				return nil, incompleteError(res, ReasonVersionIncompatible, errx.CodeUsage,
					"la versión "+v.Number+" existe, pero no está publicada (estado: "+statusText(v.Status)+")",
					"elige otra versión del proyecto")
			}
			if compat, reason := v.Compatibility(req.Env); compat != marketplace.Compatible {
				return nil, incompleteError(res, ReasonVersionIncompatible, errx.CodeUsage,
					"la versión "+v.Number+" no es compatible con "+loader+" y Minecraft "+mc+": "+reason,
					"elige otra versión o revisa el entorno de la instancia")
			}
			if !channelAllowed(v, req) {
				return nil, incompleteError(res, ReasonVersionIncompatible, errx.CodeUsage,
					"la versión "+v.Number+" pertenece al canal "+channelText(v)+", que no está aceptado",
					"acepta canales preliminares o elige una versión de lanzamiento")
			}
			return &versions[i], nil
		}
		// La versión pedida puede existir en el proyecto pero no declarar este
		// entorno: se distingue ese caso para no decir "no existe" cuando sí
		// existe.
		return nil, incompleteError(res, ReasonVersionInexistente, errx.CodeNotFound,
			"el proyecto "+displayName(res.Project)+" no tiene ninguna versión "+want+
				" para "+loader+" y Minecraft "+mc,
			"comprueba el número de versión o quita el filtro de versión exacta")
	}

	for i := range versions {
		v := versions[i]
		if !v.Published || !channelAllowed(v, req) {
			continue
		}
		if compat, _ := v.Compatibility(req.Env); compat != marketplace.Compatible {
			continue
		}
		return &versions[i], nil
	}
	return nil, incompleteError(res, ReasonSinVersion, errx.CodeUsage,
		"ninguna versión de "+displayName(res.Project)+" es compatible con "+loader+" y Minecraft "+mc+
			" en los canales aceptados ("+strings.Join(channels(req), ", ")+")",
		"acepta canales preliminares o revisa el entorno de la instancia")
}

// sameVersion compara el número o el identificador de versión.
func sameVersion(v marketplace.Version, want string) bool {
	return strings.EqualFold(strings.TrimSpace(v.Number), want) ||
		strings.EqualFold(strings.TrimSpace(v.ID), want)
}

// channelAllowed informa si el canal de la versión está aceptado.
func channelAllowed(v marketplace.Version, req Request) bool {
	if req.AllowPrerelease {
		return true
	}
	channel := strings.ToLower(strings.TrimSpace(v.Channel))
	for _, allowed := range channels(req) {
		if channel == allowed {
			return true
		}
	}
	return false
}

// channels devuelve los canales aceptados, con el valor por defecto si no se
// indicaron.
func channels(req Request) []string {
	out := marketplace.NormalizeDeclared(req.Channels)
	if len(out) == 0 {
		return ChannelsDefault
	}
	for i := range out {
		out[i] = strings.ToLower(out[i])
	}
	return out
}

// channelText describe el canal de una versión para los mensajes.
func channelText(v marketplace.Version) string {
	if c := strings.TrimSpace(v.Channel); c != "" {
		return c
	}
	return "desconocido"
}

// sortVersions ordena de la más reciente a la más antigua. Los proveedores ya
// devuelven ese orden, pero no se confía en él: la fecha publicada manda, y las
// versiones sin fecha conservan su orden relativo.
func sortVersions(versions []marketplace.Version) {
	sort.SliceStable(versions, func(i, j int) bool {
		a, b := strings.TrimSpace(versions[i].DatePublished), strings.TrimSpace(versions[j].DatePublished)
		if a == "" || b == "" {
			return false
		}
		return a > b
	})
}

// evaluateDependencies cruza las dependencias declaradas con lo instalado.
func evaluateDependencies(v marketplace.Version, installed []plugins.Record) []DependencyStatus {
	var out []DependencyStatus
	seen := map[string]bool{}
	for _, dep := range v.Dependencies {
		// Solo se deduplica lo que tiene identificador. Una dependencia sin
		// identificador NO se descarta en silencio: se conserva para poder
		// informar de que no se puede verificar.
		if key := dep.Key(); key != "" {
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		out = append(out, evaluateDependency(dep, installed))
	}
	return out
}

// evaluateDependency decide el estado de una dependencia.
func evaluateDependency(dep marketplace.Dependency, installed []plugins.Record) DependencyStatus {
	rec, found := findInstalled(dep, installed)
	switch dep.Kind {
	case marketplace.DepIncompatible:
		if found {
			return DependencyStatus{Dependency: dep, Status: DepIncompatible,
				Detail: "el proveedor la declara incompatible y está instalada como " + rec.Name}
		}
		return DependencyStatus{Dependency: dep, Status: DepSatisfecha,
			Detail: "declarada incompatible y no está instalada"}
	case marketplace.DepEmbedded:
		return DependencyStatus{Dependency: dep, Status: DepIncluida,
			Detail: "va incluida en el artefacto"}
	case marketplace.DepOptional:
		if found {
			return DependencyStatus{Dependency: dep, Status: DepOpcional,
				Detail: "opcional y ya instalada como " + rec.Name}
		}
		return DependencyStatus{Dependency: dep, Status: DepOpcional, Detail: "opcional y no instalada"}
	case marketplace.DepRequired:
		if dep.Key() == "" {
			return DependencyStatus{Dependency: dep, Status: DepNoVerificable,
				Detail: "obligatoria sin identificador: el proveedor no dice de qué proyecto depende"}
		}
		if !found {
			return DependencyStatus{Dependency: dep, Status: DepAusente,
				Detail: "obligatoria y no está instalada"}
		}
		if v := strings.TrimSpace(dep.VersionID); v != "" {
			got := strings.TrimSpace(rec.Origin.VersionID)
			if got != "" && !strings.EqualFold(got, v) {
				return DependencyStatus{Dependency: dep, Status: DepIncompatible,
					Detail: "instalada la versión " + got + " y se exige la " + v}
			}
		}
		return DependencyStatus{Dependency: dep, Status: DepSatisfecha,
			Detail: "obligatoria y ya instalada como " + rec.Name}
	default:
		if found {
			return DependencyStatus{Dependency: dep, Status: DepSatisfecha,
				Detail: "ya instalada como " + rec.Name}
		}
		return DependencyStatus{Dependency: dep, Status: DepDesconocida,
			Detail: "el proveedor no declara de qué tipo es la dependencia"}
	}
}

// findInstalled busca el registro que corresponde a una dependencia. Compara por
// identificador de proyecto, por slug y, si no hay nada mejor, por nombre de
// archivo: nunca inventa una coincidencia.
func findInstalled(dep marketplace.Dependency, installed []plugins.Record) (plugins.Record, bool) {
	wantID := strings.ToLower(strings.TrimSpace(dep.ProjectID))
	wantName := strings.ToLower(strings.TrimSpace(dep.Name))
	for _, rec := range installed {
		if wantID != "" {
			if strings.EqualFold(rec.Origin.ProjectID, wantID) || strings.EqualFold(rec.Origin.ProjectSlug, wantID) {
				return rec, true
			}
		}
		if wantName != "" {
			base := strings.ToLower(strings.TrimSuffix(rec.Name, filepathExt(rec.Name)))
			if wantName == base ||
				wantName == strings.ToLower(strings.TrimSpace(rec.Origin.ProjectName)) ||
				wantName == strings.ToLower(strings.TrimSpace(rec.Origin.ProjectSlug)) {
				return rec, true
			}
		}
	}
	return plugins.Record{}, false
}

// dependencyProblem construye el problema de dependencias no satisfechas. Las
// dependencias obligatorias ausentes se resuelven EXPLÍCITAMENTE: o fallan, o
// quedan como aviso si el llamador lo pidió. Nunca se instalan solas.
func dependencyProblem(v marketplace.Version, deps []DependencyStatus, allowMissing bool, res *Resolution) *Problem {
	var missing, incompatible, unverifiable []string
	for _, d := range deps {
		switch d.Status {
		case DepAusente:
			missing = append(missing, depName(d.Dependency))
		case DepIncompatible:
			incompatible = append(incompatible, depName(d.Dependency))
		case DepNoVerificable:
			unverifiable = append(unverifiable, depName(d.Dependency))
		}
	}
	if len(missing) == 0 && len(incompatible) == 0 && len(unverifiable) == 0 {
		return nil
	}
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, "faltan dependencias obligatorias: "+strings.Join(missing, ", "))
	}
	if len(unverifiable) > 0 {
		parts = append(parts, "hay dependencias obligatorias que el proveedor no identifica: "+
			strings.Join(unverifiable, ", "))
	}
	if len(incompatible) > 0 {
		parts = append(parts, "hay dependencias declaradas incompatibles instaladas: "+strings.Join(incompatible, ", "))
	}
	problem := &Problem{
		Reason:  ReasonDependencia,
		Message: "la versión " + v.Number + " no se puede resolver: " + strings.Join(parts, "; "),
		Hint:    "instala o retira esas dependencias explícitamente; badomens no instala dependencias por su cuenta",
		Fatal:   true,
	}
	if allowMissing && len(incompatible) == 0 {
		problem.Fatal = false
		res.Warnings = append(res.Warnings, problem.Message)
		res.Problems = append(res.Problems, *problem)
		return nil
	}
	return problem
}

// depName devuelve un nombre legible para una dependencia.
func depName(dep marketplace.Dependency) string {
	for _, candidate := range []string{dep.Name, dep.ProjectID, dep.VersionID} {
		if v := strings.TrimSpace(candidate); v != "" {
			return v
		}
	}
	return "desconocida"
}

// displayName devuelve el nombre legible del proyecto.
func displayName(p marketplace.Project) string {
	for _, candidate := range []string{p.Title, p.Slug, p.ID} {
		if v := strings.TrimSpace(candidate); v != "" {
			return v
		}
	}
	return "el proyecto"
}

// statusText evita mensajes vacíos en el estado de publicación.
func statusText(status string) string {
	if s := strings.TrimSpace(status); s != "" {
		return s
	}
	return "desconocido"
}

// engineName devuelve el motor del entorno, o el loader si no hay motor.
func engineName(env marketplace.Environment) string {
	if e := strings.TrimSpace(env.Engine); e != "" {
		return strings.ToLower(e)
	}
	return strings.ToLower(strings.TrimSpace(env.Loader))
}

// installableName informa si el archivo es un .jar o un .zip.
func installableName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return strings.HasSuffix(n, ".jar") || strings.HasSuffix(n, ".zip")
}

// filepathExt devuelve la extensión de un nombre de archivo, sin depender de
// filepath (el nombre viene de un marketplace, no del disco).
func filepathExt(name string) string {
	i := strings.LastIndex(name, ".")
	if i <= 0 {
		return ""
	}
	return name[i:]
}

// fail marca la resolución como incompleta y devuelve el error tipado.
func fail(res *Resolution, reason string, code errx.Code, message, hint string) (*Resolution, error) {
	res.Complete = false
	res.Problems = append(res.Problems, Problem{Reason: reason, Message: message, Hint: hint, Fatal: true})
	return res, typed(code, reason, message, hint)
}

// incompleteError construye solo el error, dejando que el llamador rellene la
// resolución si lo necesita.
func incompleteError(res *Resolution, reason string, code errx.Code, message, hint string) error {
	res.Complete = false
	res.Problems = append(res.Problems, Problem{Reason: reason, Message: message, Hint: hint, Fatal: true})
	return typed(code, reason, message, hint)
}

// typed construye el error tipado de resolución incompleta.
func typed(code errx.Code, reason, message, hint string) error {
	return errx.New(code, message, hint).WithDetail("reason", reason)
}
