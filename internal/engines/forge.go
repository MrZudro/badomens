package engines

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Forge (files.minecraftforge.net)
// ---------------------------------------------------------------------------
//
// Forge publica DOS catálogos que NO se pueden confundir (API-06):
//
//   - promotions_slim.json: el contrato de PROMOCIONES. Es un mapa por versión
//     de Minecraft con las claves "<mc>-recommended" y "<mc>-latest"; NO es una
//     lista de builds.
//   - maven-metadata.json: el índice real de BUILDS descargables, un mapa
//     "<mc>" → ["<mc>-<build>", …] SIN `versioning`. El parser Maven genérico,
//     que esperaba `versioning.versions`, dejaba este catálogo VACÍO en
//     silencio; aquí un documento válido con otra forma falla con
//     schema_mismatch y un catálogo vacío nunca se acepta.

// Canales de promoción del contrato de Forge.
const (
	forgeChannelRecommended = "recommended"
	forgeChannelLatest      = "latest"
)

// forgeProvider consulta las promociones y el índice Maven de builds de Forge.
type forgeProvider struct {
	engine Engine
	client *Client
	base   string
}

func (p *forgeProvider) Engine() Engine { return p.engine }

func (p *forgeProvider) Base() string {
	if p.base != "" {
		return p.base
	}
	return "https://files.minecraftforge.net/net/minecraftforge/forge"
}

func (p *forgeProvider) promotionsURL() string { return p.Base() + "/promotions_slim.json" }
func (p *forgeProvider) mavenURL() string      { return p.Base() + "/maven-metadata.json" }

// forgePromotionsDoc es el documento de promociones ANTES de validar el tipo de
// `promos`: se guarda en crudo para poder rechazar un JSON válido con otra
// estructura (schema_mismatch) en vez de quedarnos con un catálogo vacío.
type forgePromotionsDoc struct {
	Homepage string          `json:"homepage"`
	Promos   json.RawMessage `json:"promos"`
}

// forgePromotions agrupa las promociones de UNA versión de Minecraft por canal.
type forgePromotions struct {
	// Recommended es el build que Forge recomienda para esa versión.
	Recommended string
	// Latest es el build más reciente publicado para esa versión.
	Latest string
}

// forgePromo es una promoción interpretada: el build exacto que Forge propone
// para una versión de Minecraft y el canal del que procede.
type forgePromo struct {
	MC      string
	Build   string
	Channel string
}

// Pin devuelve el valor exacto que espera la imagen de itzg en FORGE_VERSION.
func (p forgePromo) Pin() string { return p.MC + "-" + p.Build }

// ordered devuelve las promociones de una versión de Minecraft en orden de
// preferencia: primero `recommended` y después `latest`.
func (f forgePromotions) ordered(mc string) []forgePromo {
	out := make([]forgePromo, 0, 2)
	if build := strings.TrimSpace(f.Recommended); build != "" {
		out = append(out, forgePromo{MC: mc, Build: build, Channel: forgeChannelRecommended})
	}
	if build := strings.TrimSpace(f.Latest); build != "" {
		out = append(out, forgePromo{MC: mc, Build: build, Channel: forgeChannelLatest})
	}
	return out
}

// preferred devuelve la promoción preferida: la recomendada o, si no hay,
// la última publicada.
func (f forgePromotions) preferred(mc string) (forgePromo, bool) {
	candidates := f.ordered(mc)
	if len(candidates) == 0 {
		return forgePromo{}, false
	}
	return candidates[0], true
}

// forgePromoKey reconoce las claves válidas del contrato de promociones.
var forgePromoKey = regexp.MustCompile(`^([0-9][0-9A-Za-z._+-]*)-(recommended|latest)$`)

// forgeParsePromotions valida y normaliza el contrato de promociones. Rechaza
// con un error tipado todo documento que no encaje en el esquema, en vez de
// devolver un catálogo vacío en silencio.
func forgeParsePromotions(doc forgePromotionsDoc, rawURL string) (map[string]forgePromotions, error) {
	fail := func(code string, cause error) (map[string]forgePromotions, error) {
		return nil, newProviderError(code, Forge, "list_promotions", rawURL, cause)
	}
	if len(bytes.TrimSpace(doc.Promos)) == 0 {
		return fail(CodeSchemaMismatch, fmt.Errorf("promotions_slim.json no trae el objeto `promos`"))
	}
	var raw map[string]string
	if err := json.Unmarshal(doc.Promos, &raw); err != nil {
		return fail(CodeSchemaMismatch, fmt.Errorf("`promos` no es un mapa de build por clave \"<mc>-<canal>\": %w", err))
	}
	if len(raw) == 0 {
		return fail(CodeInvalidResponse, fmt.Errorf("promotions_slim.json no trae ninguna promoción (catálogo vacío)"))
	}
	out := make(map[string]forgePromotions, len(raw)/2+1)
	matched := 0
	for key, build := range raw {
		m := forgePromoKey.FindStringSubmatch(strings.TrimSpace(key))
		if m == nil {
			continue // clave desconocida: se ignora, no se interpreta
		}
		matched++
		mc, channel := m[1], m[2]
		build = strings.TrimSpace(build)
		if build == "" {
			continue
		}
		promo := out[mc]
		if channel == forgeChannelRecommended {
			promo.Recommended = build
		} else {
			promo.Latest = build
		}
		out[mc] = promo
	}
	if matched == 0 {
		return fail(CodeSchemaMismatch, fmt.Errorf("`promos` no tiene ninguna clave con el formato \"<mc>-recommended\" o \"<mc>-latest\""))
	}
	if len(out) == 0 {
		return fail(CodeInvalidResponse, fmt.Errorf("promotions_slim.json no trae ningún build utilizable"))
	}
	return out, nil
}

// promotions consulta y valida las promociones oficiales.
func (p *forgeProvider) promotions(ctx context.Context) (map[string]forgePromotions, error) {
	u := p.promotionsURL()
	var doc forgePromotionsDoc
	if _, err := p.client.GetJSONWithMeta(ctx, u, &doc, RequestOpts{
		Provider:  Forge.Name,
		Operation: "list_promotions",
	}); err != nil {
		return nil, err
	}
	return forgeParsePromotions(doc, u)
}

// forgeMavenIndex es el catálogo REAL de builds descargables de Forge:
// "<mc>" → ["<mc>-<build>", …]. Un documento con otra forma no se puede
// decodificar como este tipo y falla con schema_mismatch.
type forgeMavenIndex map[string][]string

// downloadable indica si un pin "<mc>-<build>" está en el índice de builds
// descargables. Es el contraste contra el artefacto real que exigen AC-03 y
// QC-04 antes de ofrecer una versión.
func (idx forgeMavenIndex) downloadable(mc, pin string) bool {
	for _, build := range idx[mc] {
		if strings.TrimSpace(build) == pin {
			return true
		}
	}
	return false
}

// owner devuelve la versión de Minecraft a la que pertenece un pin
// "<mc>-<build>" presente en el índice de builds.
func (idx forgeMavenIndex) owner(pin string) (string, bool) {
	for mc, builds := range idx {
		for _, build := range builds {
			if strings.TrimSpace(build) == pin {
				return mc, true
			}
		}
	}
	return "", false
}

// mavenIndex consulta el índice de builds descargables y rechaza los catálogos
// vacíos: vacío en silencio es exactamente el fallo que documenta API-06.
func (p *forgeProvider) mavenIndex(ctx context.Context) (forgeMavenIndex, error) {
	u := p.mavenURL()
	var idx forgeMavenIndex
	if _, err := p.client.GetJSONWithMeta(ctx, u, &idx, RequestOpts{
		Provider:  Forge.Name,
		Operation: "list_builds",
	}); err != nil {
		return nil, err
	}
	total := 0
	for _, builds := range idx {
		total += len(builds)
	}
	if len(idx) == 0 || total == 0 {
		return nil, newProviderError(CodeSchemaMismatch, Forge, "list_builds", u,
			fmt.Errorf("el índice Maven de builds llegó vacío: se rechaza en vez de publicar un catálogo sin builds descargables"))
	}
	return idx, nil
}

// ListVersions devuelve una entrada por versión de Minecraft con su promoción
// oficial en Build (la recomendada o, si no hay, la última). La lista se ordena
// de la más nueva a la más antigua comparando versiones: el orden del mapa en Go
// es aleatorio y no puede filtrarse a la salida.
func (p *forgeProvider) ListVersions(ctx context.Context) ([]Version, error) {
	promos, err := p.promotions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Version, 0, len(promos))
	for mc := range promos {
		promo, ok := promos[mc].preferred(mc)
		if !ok {
			continue
		}
		out = append(out, Version{
			Version:       promo.MC,
			Stable:        true,
			Channel:       ChannelRelease,
			Build:         promo.Build,
			Detail:        promo.Pin() + " (" + promo.Channel + ")",
			Compatibility: CompatibilityCompatible,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return compareProviderVersions(out[i].Version, out[j].Version) > 0
	})
	return out, nil
}

// forgeSelectMC elige la versión de Minecraft a resolver: la pedida (debe tener
// promociones) o la más reciente que las tenga.
func forgeSelectMC(promos map[string]forgePromotions, requested string) (string, error) {
	if requested != "" {
		if _, ok := promos[requested]; !ok {
			return "", newProviderError(CodeNotFound, Forge, "resolve", "",
				fmt.Errorf("Forge no publica promociones para la versión de Minecraft %q", requested))
		}
		return requested, nil
	}
	best := ""
	for mc := range promos {
		if best == "" || compareProviderVersions(mc, best) > 0 {
			best = mc
		}
	}
	if best == "" {
		return "", newProviderError(CodeInvalidResponse, Forge, "resolve", "",
			fmt.Errorf("no hay ninguna versión de Minecraft con promociones publicadas"))
	}
	return best, nil
}

// Resolve fija FORGE_VERSION con el valor exacto "<mc>-<build>" de un artefacto
// DESCARGABLE (AC-03). Criterio:
//
//  1. Si se pide un pin completo "<mc>-<build>" que esté en el índice Maven, se
//     acepta tal cual.
//  2. Si se pide (o se deduce) una versión de Minecraft, se toma su promoción
//     oficial por orden de preferencia: `recommended` y, si su build no está en
//     el índice, `latest`.
//  3. Si ninguna de las promociones es descargable, la resolución FALLA: nunca
//     se devuelve un pin sin artefacto (T-02/QC-04).
func (p *forgeProvider) Resolve(ctx context.Context, version string) (Resolved, error) {
	version = strings.TrimSpace(version)
	idx, err := p.mavenIndex(ctx)
	if err != nil {
		return Resolved{}, err
	}
	if version != "" {
		if mc, ok := idx.owner(version); ok {
			return Resolved{
				Version: mc,
				Pins:    map[string]string{Forge.DepVar: version},
				Detail:  version,
			}, nil
		}
	}
	promos, err := p.promotions(ctx)
	if err != nil {
		return Resolved{}, err
	}
	mc, err := forgeSelectMC(promos, version)
	if err != nil {
		return Resolved{}, err
	}
	candidates := promos[mc].ordered(mc)
	if len(candidates) == 0 {
		return Resolved{}, newProviderError(CodeInvalidResponse, Forge, "resolve", "",
			fmt.Errorf("no hay ninguna promoción utilizable para la versión de Minecraft %q", mc))
	}
	tried := make([]string, 0, len(candidates))
	for _, cand := range candidates {
		if idx.downloadable(cand.MC, cand.Pin()) {
			return Resolved{
				Version: cand.MC,
				Pins:    map[string]string{Forge.DepVar: cand.Pin()},
				Detail:  cand.Pin() + " (" + cand.Channel + ")",
			}, nil
		}
		tried = append(tried, cand.Pin())
	}
	return Resolved{}, newProviderError(CodeNotFound, Forge, "resolve", p.mavenURL(),
		fmt.Errorf("ninguno de los builds publicados para Minecraft %s está en el índice de builds descargables (%s)",
			mc, strings.Join(tried, ", ")))
}
