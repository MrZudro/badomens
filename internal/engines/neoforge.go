package engines

import (
	"context"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// NeoForge (maven.neoforged.net, XML oficial)
// ---------------------------------------------------------------------------
//
// El catálogo oficial es XML Maven (maven-metadata.xml). El endpoint
// `maven-metadata.json` que usaba el parser compartido devuelve 404 (API-06):
// aquí se parsea el XML, con un parser PROPIO y sin compartirlo con Forge (T-06).
//
// Coexisten DOS esquemas de nomenclatura:
//
//	antiguo (Minecraft 1.x):  "21.1.72"      ↔ Minecraft "1.21.1"
//	moderno (desde 26.1):     "26.1.0.5-beta" ↔ Minecraft "26.1"
//
// La asociación Minecraft ↔ NeoForge es EXPLÍCITA (neoforgeAssociate), no una
// división de cadena genérica, y nunca se ordenan alfabéticamente versiones de
// esquemas distintos.

// Códigos estables de las advertencias propias de NeoForge.
const (
	// CodeCatalogDrop: el catálogo publicado cayó drásticamente respecto al
	// último validado. Se conserva el previo y se advierte.
	CodeCatalogDrop = "catalog_drop"
	// CodeCatalogUnmapped: hay versiones publicadas cuya nomenclatura no se puede
	// asociar a una versión de Minecraft, así que su compatibilidad no se puede
	// comprobar.
	CodeCatalogUnmapped = "catalog_unmapped"
)

// Parámetros de la detección de caídas anómalas.
const (
	// neoforgeMinBaseline es el tamaño mínimo del último catálogo validado para
	// poder juzgar una caída como anómala (evita falsos positivos en catálogos
	// pequeños o recién nacidos).
	neoforgeMinBaseline = 20
	// neoforgeDropRatio: si el catálogo nuevo conserva menos de esta fracción de
	// las versiones del último validado, la caída es anómala. La regresión real
	// de sep-2026 dejó 3 versiones donde había más de 1800.
	neoforgeDropRatio = 0.5
	// neoforgeModernMajor es el primer componente de la versión de Minecraft del
	// esquema moderno de NeoForge (Minecraft 26.1 ↔ NeoForge 26.1.x.y).
	neoforgeModernMajor = 26
)

// CatalogWarning es una advertencia legible con código estable sobre la
// fiabilidad de un catálogo. Según el código, Previous/Current/Count documentan
// los datos que la motivan.
type CatalogWarning struct {
	// Code es el código estable (catalog_drop, catalog_unmapped…).
	Code string `json:"code"`
	// Message es el texto legible para personas.
	Message string `json:"message"`
	// Count es el número de elementos afectados.
	Count int `json:"count,omitempty"`
	// Previous y Current son el tamaño del catálogo previo y del recibido.
	Previous int `json:"previous,omitempty"`
	Current  int `json:"current,omitempty"`
}

// NeoForgeCatalog es el resultado de consultar el catálogo XML oficial. Es la vía
// por la que el proveedor propaga la incertidumbre: la interfaz Provider no
// puede cambiar sus firmas en esta fase, así que la advertencia viaja aquí y en
// Warnings().
type NeoForgeCatalog struct {
	// Versions son las versiones UTILIZABLES: una entrada por versión de
	// Minecraft, con el pin exacto de NeoForge en Build. Si se detectó una caída
	// anómala, contiene el último catálogo validado (Retained=true).
	Versions []Version
	// Warnings son las advertencias de esta consulta (vacío si es fiable).
	Warnings []CatalogWarning
	// Retained indica que Versions es el último catálogo VALIDADO y no lo
	// publicado en esta consulta. No se ha sobrescrito el catálogo conservado.
	Retained bool
	// PreviousCount es el tamaño (número de versiones publicadas) del último
	// catálogo validado antes de esta consulta; 0 si no había ninguno.
	PreviousCount int
	// Unmapped cuenta las versiones publicadas en ESTA consulta cuya
	// nomenclatura no se pudo asociar a una versión de Minecraft.
	Unmapped int
}

// Warning devuelve el texto legible de la primera advertencia ("" si no hay).
func (c NeoForgeCatalog) Warning() string {
	if len(c.Warnings) == 0 {
		return ""
	}
	return c.Warnings[0].Message
}

// neoforgeMetadata es el documento XML oficial. `versioning` es un puntero para
// poder distinguir "el documento no trae <versioning>" (schema_mismatch) de
// "el catálogo está vacío" (invalid_response).
type neoforgeMetadata struct {
	XMLName    xml.Name `xml:"metadata"`
	GroupID    string   `xml:"groupId"`
	ArtifactID string   `xml:"artifactId"`
	Versioning *struct {
		Latest   string   `xml:"latest"`
		Release  string   `xml:"release"`
		Versions []string `xml:"versions>version"`
	} `xml:"versioning"`
}

// neoforgeProvider consulta el XML oficial de NeoForge.
type neoforgeProvider struct {
	engine Engine
	client *Client
	base   string

	mu sync.Mutex
	// baseline es el último catálogo VALIDADO (una entrada por versión de
	// Minecraft). No se sobrescribe cuando llega una caída anómala.
	baseline []Version
	// baselinePublished es el número de versiones publicadas del último catálogo
	// validado: la métrica con la que se juzgan las caídas.
	baselinePublished int
	haveBaseline      bool
	// warned son las advertencias de la última consulta, para los consumidores
	// que entran por la interfaz Provider (que no devuelve advertencias).
	warned []CatalogWarning
}

func (p *neoforgeProvider) Engine() Engine { return p.engine }

func (p *neoforgeProvider) Base() string {
	if p.base != "" {
		return p.base
	}
	return "https://maven.neoforged.net/releases/net/neoforged/neoforge"
}

// metadataURL es el XML OFICIAL (nunca maven-metadata.json, que devuelve 404).
func (p *neoforgeProvider) metadataURL() string { return p.Base() + "/maven-metadata.xml" }

// neoforgeScheme identifica el esquema de nomenclatura de una versión de NeoForge.
type neoforgeScheme string

const (
	// neoforgeSchemeLegacy: esquema antiguo, Minecraft 1.x ↔ NeoForge "x.y.build".
	neoforgeSchemeLegacy neoforgeScheme = "legacy"
	// neoforgeSchemeModern: esquema moderno, Minecraft 26.x ↔ NeoForge "26.x.y.build".
	neoforgeSchemeModern neoforgeScheme = "modern"
	// neoforgeSchemeUnknown: nomenclatura no reconocida.
	neoforgeSchemeUnknown neoforgeScheme = "unknown"
)

// neoforgeSplitQualifier separa la parte numérica de la versión de sus
// calificadores ("-beta", "-alpha.10", "+snapshot-6").
func neoforgeSplitQualifier(neo string) (base, qualifier string) {
	if i := strings.IndexAny(neo, "-+"); i >= 0 {
		return neo[:i], neo[i:]
	}
	return neo, ""
}

// neoforgeAssociate asocia EXPLÍCITAMENTE una versión de NeoForge con su versión
// de Minecraft y con su esquema de nomenclatura. Es una regla declarada, no una
// división de cadena genérica:
//
//	esquema antiguo (MC 1.x):  "20.2.29-beta" → 1.20.2 · "21.1.72" → 1.21.1
//	esquema moderno (MC 26.x): "26.1.0.5-beta" → 26.1 · "26.3.0.77" → 26.3
//
// Devuelve esquema desconocido y mc vacío cuando la nomenclatura no permite
// comprobar la compatibilidad (por ejemplo "0.25w14craftmine.3-beta").
func neoforgeAssociate(neo string) (neoforgeScheme, string) {
	base, _ := neoforgeSplitQualifier(neo)
	parts := strings.Split(base, ".")
	for _, part := range parts {
		if !isNumericToken(part) {
			return neoforgeSchemeUnknown, ""
		}
	}
	switch len(parts) {
	case 3: // esquema antiguo: "21.1.72"
		major, err := strconv.Atoi(parts[0])
		if err != nil || major < 20 || major > 25 {
			return neoforgeSchemeUnknown, ""
		}
		return neoforgeSchemeLegacy, "1." + parts[0] + "." + parts[1]
	case 4: // esquema moderno: "26.1.0.5"
		major, err := strconv.Atoi(parts[0])
		if err != nil || major < neoforgeModernMajor {
			return neoforgeSchemeUnknown, ""
		}
		return neoforgeSchemeModern, parts[0] + "." + parts[1]
	}
	return neoforgeSchemeUnknown, ""
}

// neoforgeStability clasifica la estabilidad REAL de una versión de NeoForge y su
// canal. Nada que lleve un calificador de pre-release se marca estable.
func neoforgeStability(neo string) (bool, Channel) {
	_, qualifier := neoforgeSplitQualifier(neo)
	q := strings.ToLower(qualifier)
	switch {
	case q == "":
		return true, ChannelRelease
	case strings.Contains(q, "alpha"):
		return false, ChannelAlpha
	case strings.Contains(q, "beta"):
		return false, ChannelBeta
	default:
		// rc, snapshot, pre, milestone…: pre-release sin canal propio.
		return false, ChannelSnapshot
	}
}

// neoforgeBetter indica si `cand` es mejor pin que `prev` para la MISMA versión
// de Minecraft: primero la estabilidad real y, a igualdad, la versión más
// reciente comparada numéricamente (nunca alfabéticamente).
func neoforgeBetter(cand, prev Version) bool {
	if cand.Stable != prev.Stable {
		return cand.Stable
	}
	return compareProviderVersions(cand.Build, prev.Build) > 0
}

// neoforgeBuildCatalog transforma las versiones publicadas en el catálogo
// normalizado: UNA entrada por versión de Minecraft, con el build exacto (el pin)
// en Build. Conserva el orden de publicación del upstream y NUNCA reordena
// alfabéticamente entre esquemas distintos. Devuelve además las versiones cuya
// nomenclatura no se pudo asociar.
func neoforgeBuildCatalog(published []string) ([]Version, []string) {
	byMC := make(map[string]Version, len(published))
	order := make([]string, 0, len(published))
	var unmapped []string
	for _, raw := range published {
		scheme, mc := neoforgeAssociate(raw)
		if scheme == neoforgeSchemeUnknown || mc == "" {
			unmapped = append(unmapped, raw)
			continue
		}
		stable, channel := neoforgeStability(raw)
		cand := Version{Version: mc, Build: raw, Stable: stable, Channel: channel, Detail: raw}
		prev, ok := byMC[mc]
		if !ok {
			order = append(order, mc)
			byMC[mc] = cand
			continue
		}
		if neoforgeBetter(cand, prev) {
			byMC[mc] = cand
		}
	}
	out := make([]Version, 0, len(order))
	for _, mc := range order {
		out = append(out, byMC[mc])
	}
	return out, unmapped
}

// Catalog consulta el XML oficial y devuelve el catálogo utilizable junto con sus
// advertencias. Detección de caída anómala (AC-04): si el número de versiones
// publicadas cae drásticamente respecto al último catálogo validado, el catálogo
// es sospechoso: se devuelve el ÚLTIMO VALIDADO (Retained=true) con una
// advertencia legible y NO se sobrescribe el conservado.
func (p *neoforgeProvider) Catalog(ctx context.Context) (NeoForgeCatalog, error) {
	u := p.metadataURL()
	var md neoforgeMetadata
	if _, err := p.client.GetXMLWithMeta(ctx, u, &md, RequestOpts{
		Provider:  NeoForge.Name,
		Operation: "list_versions",
	}); err != nil {
		return NeoForgeCatalog{}, err
	}
	if md.Versioning == nil {
		return NeoForgeCatalog{}, newProviderError(CodeSchemaMismatch, NeoForge, "list_versions", u,
			fmt.Errorf("el XML no trae el bloque <versioning>: no es el maven-metadata.xml de NeoForge"))
	}
	published := make([]string, 0, len(md.Versioning.Versions))
	for _, v := range md.Versioning.Versions {
		if v = strings.TrimSpace(v); v != "" {
			published = append(published, v)
		}
	}
	if len(published) == 0 {
		return NeoForgeCatalog{}, newProviderError(CodeInvalidResponse, NeoForge, "list_versions", u,
			fmt.Errorf("<versioning> no publica ninguna <version>"))
	}
	versions, unmapped := neoforgeBuildCatalog(published)
	if len(versions) == 0 {
		return NeoForgeCatalog{}, newProviderError(CodeInvalidResponse, NeoForge, "list_versions", u,
			fmt.Errorf("ninguna de las %d versiones publicadas se pudo asociar a una versión de Minecraft", len(published)))
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	prevCount := p.baselinePublished
	if p.haveBaseline && p.baselinePublished >= neoforgeMinBaseline &&
		float64(len(published)) < float64(p.baselinePublished)*neoforgeDropRatio {
		warning := CatalogWarning{
			Code: CodeCatalogDrop,
			Message: fmt.Sprintf("catálogo sospechoso: NeoForge publicó %d versiones frente a %d del último catálogo validado; se conserva el catálogo previo y no se sobrescribe",
				len(published), p.baselinePublished),
			Count:    len(published),
			Previous: p.baselinePublished,
			Current:  len(published),
		}
		cat := NeoForgeCatalog{
			Versions:      append([]Version(nil), p.baseline...),
			Warnings:      []CatalogWarning{warning},
			Retained:      true,
			PreviousCount: prevCount,
			Unmapped:      len(unmapped),
		}
		p.warned = append([]CatalogWarning(nil), cat.Warnings...)
		return cat, nil
	}

	p.baseline = append([]Version(nil), versions...)
	p.baselinePublished = len(published)
	p.haveBaseline = true
	cat := NeoForgeCatalog{
		Versions:      versions,
		PreviousCount: prevCount,
		Unmapped:      len(unmapped),
	}
	if len(unmapped) > 0 {
		cat.Warnings = append(cat.Warnings, CatalogWarning{
			Code: CodeCatalogUnmapped,
			Message: fmt.Sprintf("%d versión(es) publicadas no se pueden asociar a una versión de Minecraft (%s): su compatibilidad no se puede comprobar",
				len(unmapped), sampleVersions(unmapped, 3)),
			Count: len(unmapped),
		})
	}
	p.warned = append([]CatalogWarning(nil), cat.Warnings...)
	return cat, nil
}

// SeedCatalog fija el último catálogo validado (por ejemplo el recuperado de la
// caché persistente) para que la detección de caídas funcione ya en la primera
// consulta del proceso. No escribe nada en disco.
func (p *neoforgeProvider) SeedCatalog(versions []Version, publishedCount int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.baseline = append([]Version(nil), versions...)
	p.baselinePublished = publishedCount
	p.haveBaseline = publishedCount > 0
}

// Warnings devuelve las advertencias de la última consulta del catálogo (vacío si
// fue fiable). Permite propagar la incertidumbre a quien entra por la interfaz
// Provider, cuyas firmas no devuelven advertencias.
func (p *neoforgeProvider) Warnings() []CatalogWarning {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]CatalogWarning(nil), p.warned...)
}

// ListVersions devuelve el catálogo normalizado (una entrada por versión de
// Minecraft). Las advertencias se obtienen con Catalog o Warnings.
func (p *neoforgeProvider) ListVersions(ctx context.Context) ([]Version, error) {
	cat, err := p.Catalog(ctx)
	if err != nil {
		return nil, err
	}
	return cat.Versions, nil
}

// Resolve fija NEOFORGE_VERSION con el build exacto de NeoForge para la versión
// de Minecraft pedida o, si no se pide ninguna, para la más reciente con build
// estable. Acepta también un build exacto de NeoForge.
func (p *neoforgeProvider) Resolve(ctx context.Context, version string) (Resolved, error) {
	cat, err := p.Catalog(ctx)
	if err != nil {
		return Resolved{}, err
	}
	version = strings.TrimSpace(version)
	if version != "" {
		for _, v := range cat.Versions {
			if v.Build == version {
				return neoforgeResolved(v, cat), nil
			}
		}
	}
	var target Version
	if version != "" {
		for _, v := range cat.Versions {
			if v.Version == version {
				target = v
				break
			}
		}
		if target.Version == "" {
			return Resolved{}, newProviderError(CodeNotFound, NeoForge, "resolve", p.metadataURL(),
				fmt.Errorf("NeoForge no publica ningún build asociado a la versión de Minecraft %q", version))
		}
	} else {
		for _, v := range cat.Versions {
			if !v.Stable {
				continue
			}
			if target.Version == "" || compareProviderVersions(v.Version, target.Version) > 0 {
				target = v
			}
		}
		if target.Version == "" {
			// Ninguna versión de Minecraft tiene build estable: se resuelve con la
			// más reciente y se advierte en el detalle (degradación explícita).
			for _, v := range cat.Versions {
				if target.Version == "" || compareProviderVersions(v.Version, target.Version) > 0 {
					target = v
				}
			}
		}
	}
	return neoforgeResolved(target, cat), nil
}

// neoforgeResolved compone la resolución con el pin exacto. De las advertencias
// del catálogo solo se anexa a Detail la que afecta a la resolución (catálogo
// conservado por caída anómala); las de ámbito de catálogo (nomenclatura no
// reconocida) se consultan con Catalog/Warnings, porque Resolved no tiene un
// campo de advertencia.
func neoforgeResolved(v Version, cat NeoForgeCatalog) Resolved {
	res := Resolved{
		Version: v.Version,
		Pins:    map[string]string{NeoForge.DepVar: v.Build},
		Detail:  v.Build,
	}
	if !v.Stable {
		res.Detail += " (" + v.Channel.String() + ", sin build estable para Minecraft " + v.Version + ")"
	}
	for _, w := range cat.Warnings {
		if w.Code == CodeCatalogDrop {
			res.Detail += " · aviso: " + w.Message
		}
	}
	return res
}

// sampleVersions devuelve hasta n versiones separadas por comas, para mensajes
// legibles y acotados.
func sampleVersions(list []string, n int) string {
	if len(list) <= n {
		return strings.Join(list, ", ")
	}
	return strings.Join(list[:n], ", ") + ", …"
}
