package engines

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// ---------------------------------------------------------------------------
// Ayudas comunes a los adaptadores de proveedor de la Fase 2
// ---------------------------------------------------------------------------
//
// Son locales a fabric.go/forge.go/neoforge.go y llevan nombre propio a
// propósito: la Fase 3 reescribe la resolución común (pickVersion,
// resolveMaven, normalizeMaven…) y esta fase NO puede depender de sus
// supuestos. En particular, aquí nunca se asume que "el último elemento de la
// lista es el más reciente": Fabric Meta entrega sus listas con las versiones
// MÁS NUEVAS PRIMERO (newest-first), de modo que ese supuesto (API-05) devuelve
// la más antigua.

// newProviderError construye un error tipado del catálogo con un código estable.
// Se usa para los fallos que detecta el propio adaptador (contrato inesperado,
// versión inexistente, build no descargable, loader no confirmado…) y no para
// los del transporte, que ya vienen tipados. Nunca lleva el cuerpo de la
// respuesta.
func newProviderError(code string, engine Engine, operation, rawURL string, cause error) *TransportError {
	return &TransportError{
		Code:      code,
		Provider:  engine.Name,
		Operation: operation,
		URL:       sanitizeURL(rawURL),
		Err:       cause,
	}
}

// compareProviderVersions compara dos identificadores de versión componente a
// componente, de forma independiente del orden en que el proveedor los publique:
//
//   - Los componentes numéricos se comparan como NÚMEROS: "1.21.10" > "1.21.9" y
//     "20.2.29-beta" > "20.2.9-beta" (la comparación alfabética diría lo
//     contrario en ambos casos).
//   - Los componentes alfabéticos se comparan como texto, sin distinguir
//     mayúsculas de minúsculas.
//   - Ante el mismo prefijo, la versión con sufijo ("-beta", "-alpha.10",
//     "+snapshot-1") ordena POR DEBAJO de la versión base.
//
// Devuelve negativo si a < b, 0 si son equivalentes y positivo si a > b.
func compareProviderVersions(a, b string) int {
	ta, tb := providerVersionTokens(a), providerVersionTokens(b)
	n := len(ta)
	if len(tb) < n {
		n = len(tb)
	}
	for i := 0; i < n; i++ {
		x, y := ta[i], tb[i]
		if isNumericToken(x) && isNumericToken(y) {
			if c := compareNumericTokens(x, y); c != 0 {
				return c
			}
			continue
		}
		xl, yl := strings.ToLower(x), strings.ToLower(y)
		if xl != yl {
			return strings.Compare(xl, yl)
		}
	}
	if len(ta) == len(tb) {
		return 0
	}
	// Una lista es prefijo de la otra: deciden los tokens sobrantes.
	longTail, sign := ta[n:], 1
	if len(tb) > len(ta) {
		longTail, sign = tb[n:], -1
	}
	for _, tok := range longTail {
		switch {
		case isNumericToken(tok):
			// Componente numérico adicional: la versión más larga es mayor
			// ("1.21.1" > "1.21").
			return sign
		case isQualifierToken(tok):
			// Calificador de pre-release: la versión más larga es MENOR
			// ("26.3.0.77-beta" < "26.3.0.77").
			return -sign
		}
	}
	return 0
}

// providerVersionTokens trocea una versión en runs consecutivos de dígitos y de
// no dígitos ("26.1.0.5-beta" → ["26", ".", "1", ".", "0", ".", "5", "-beta"]).
func providerVersionTokens(s string) []string {
	var out []string
	start := 0
	prevDigit := false
	for i := 0; i < len(s); i++ {
		digit := isDigitByte(s[i])
		if i == 0 {
			prevDigit = digit
			continue
		}
		if digit != prevDigit {
			out = append(out, s[start:i])
			start = i
			prevDigit = digit
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// isNumericToken indica si un token está formado solo por dígitos.
func isNumericToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isDigitByte(s[i]) {
			return false
		}
	}
	return true
}

// isQualifierToken indica si un token abre un calificador de pre-release
// ("-beta", "+snapshot-1", "_pre4").
func isQualifierToken(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return !isDigitByte(c) && c != '.'
}

// isDigitByte reconoce un dígito ASCII.
func isDigitByte(c byte) bool { return c >= '0' && c <= '9' }

// compareNumericTokens compara dos tokens numéricos como números, sin desbordar:
// primero por longitud (quitando ceros a la izquierda) y luego lexicográficamente.
func compareNumericTokens(x, y string) int {
	x = strings.TrimLeft(x, "0")
	y = strings.TrimLeft(y, "0")
	if len(x) != len(y) {
		if len(x) < len(y) {
			return -1
		}
		return 1
	}
	return strings.Compare(x, y)
}

// ---------------------------------------------------------------------------
// Fabric (meta.fabricmc.net v2)
// ---------------------------------------------------------------------------

// fabricProvider consulta la meta de FabricMC (juego y loader).
type fabricProvider struct {
	engine Engine
	client *Client
	base   string
}

func (p *fabricProvider) Engine() Engine { return p.engine }

func (p *fabricProvider) Base() string {
	if p.base != "" {
		return p.base
	}
	return "https://meta.fabricmc.net/v2"
}

// fabricGame es una versión de Minecraft publicada por Fabric Meta
// (GET /v2/versions/game).
type fabricGame struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

// fabricLoaderEntry es una entrada del catálogo de loaders ESPECÍFICO de una
// versión de Minecraft (GET /v2/versions/loader/{game_version}). La versión va
// ANIDADA en `loader`, a diferencia del catálogo general, donde va en la raíz:
// son formas distintas y solo esta garantiza que el loader es compatible con la
// versión de Minecraft pedida (API-04).
type fabricLoaderEntry struct {
	Loader struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	} `json:"loader"`
}

// games consulta el catálogo de versiones de Minecraft.
func (p *fabricProvider) games(ctx context.Context) ([]fabricGame, error) {
	u := p.Base() + "/versions/game"
	var games []fabricGame
	if _, err := p.client.GetJSONWithMeta(ctx, u, &games, RequestOpts{
		Provider:  Fabric.Name,
		Operation: "list_game_versions",
	}); err != nil {
		return nil, err
	}
	if len(games) == 0 {
		return nil, newProviderError(CodeInvalidResponse, Fabric, "list_game_versions", u,
			fmt.Errorf("el catálogo de Fabric Meta no devolvió ninguna versión de Minecraft"))
	}
	return games, nil
}

// ListVersions devuelve las versiones de Minecraft de Fabric Meta CONSERVANDO el
// orden del proveedor. Fabric Meta las entrega newest-first (la más nueva
// primero), así que el orden no se puede usar como criterio de "más reciente":
// para eso está compareProviderVersions.
func (p *fabricProvider) ListVersions(ctx context.Context) ([]Version, error) {
	games, err := p.games(ctx)
	if err != nil {
		return nil, err
	}
	versions := make([]Version, 0, len(games))
	for _, g := range games {
		// La propia meta de Fabric publica la versión del juego: es la verificación
		// de compatibilidad con el motor (trivalente: nunca se asume).
		versions = append(versions, Version{
			Version:       g.Version,
			Stable:        g.Stable,
			Compatibility: CompatibilityCompatible,
		})
	}
	return versions, nil
}

// fabricLatestStable elige la versión de Minecraft estable MÁS RECIENTE del
// catálogo, comparando versiones y no posiciones: la respuesta de Fabric Meta
// viene newest-first, pero un cambio de orden (o una respuesta mezclada) no debe
// alterar el resultado.
func fabricLatestStable(games []fabricGame) (string, bool) {
	best := ""
	for _, g := range games {
		v := strings.TrimSpace(g.Version)
		if v == "" || !g.Stable {
			continue
		}
		if best == "" || compareProviderVersions(v, best) > 0 {
			best = v
		}
	}
	return best, best != ""
}

// fabricPickGame devuelve la versión de Minecraft pedida (que debe existir en el
// catálogo real) o, si no se pidió ninguna, la última estable.
func fabricPickGame(games []fabricGame, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		for _, g := range games {
			if g.Version == requested {
				return g.Version, nil
			}
		}
		return "", newProviderError(CodeNotFound, Fabric, "resolve", "",
			fmt.Errorf("la versión de Minecraft %q no existe en Fabric Meta", requested))
	}
	if v, ok := fabricLatestStable(games); ok {
		return v, nil
	}
	return "", newProviderError(CodeInvalidResponse, Fabric, "resolve", "",
		fmt.Errorf("el catálogo de Fabric Meta no publica ninguna versión de Minecraft estable"))
}

// loadersFor consulta los loaders COMPATIBLES con una versión de Minecraft en el
// endpoint específico /v2/versions/loader/{game_version}.
func (p *fabricProvider) loadersFor(ctx context.Context, mc string) ([]fabricLoaderEntry, error) {
	u := p.Base() + "/versions/loader/" + url.PathEscape(mc)
	var entries []fabricLoaderEntry
	if _, err := p.client.GetJSONWithMeta(ctx, u, &entries, RequestOpts{
		Provider:  Fabric.Name,
		Operation: "list_loaders",
	}); err != nil {
		return nil, err
	}
	return entries, nil
}

// fabricPickLoader fija el loader EXACTO aplicando la política del proyecto:
// solo se aceptan loaders marcados como estables por Fabric Meta y, entre ellos,
// se elige el más reciente por comparación de versiones (nunca por su posición
// en la lista). Si no hay ningún loader estable, se rechaza la resolución.
func fabricPickLoader(entries []fabricLoaderEntry) (string, error) {
	best := ""
	for _, e := range entries {
		v := strings.TrimSpace(strings.TrimPrefix(e.Loader.Version, "v"))
		if v == "" || !e.Loader.Stable {
			continue
		}
		if best == "" || compareProviderVersions(v, best) > 0 {
			best = v
		}
	}
	if best == "" {
		return "", fmt.Errorf("el catálogo de Fabric Meta no confirmó ningún loader estable (%d loader(s) recibidos)", len(entries))
	}
	return best, nil
}

// Resolve ejecuta el flujo obligatorio (API-04, AC-02):
//
//  1. elegir una versión REAL de Minecraft (la pedida o la última estable);
//  2. consultar GET /v2/versions/loader/{game_version} (nunca el catálogo
//     general /v2/versions/loader, que no garantiza compatibilidad);
//  3. filtrar los loaders inestables según la política;
//  4. fijar el loader exacto.
//
// Si el loader no se confirma, la resolución FALLA: no existe una resolución
// parcial sin FABRIC_LOADER_VERSION (T-02/AC-06).
func (p *fabricProvider) Resolve(ctx context.Context, version string) (Resolved, error) {
	games, err := p.games(ctx)
	if err != nil {
		return Resolved{}, err
	}
	mc, err := fabricPickGame(games, version)
	if err != nil {
		return Resolved{}, err
	}
	entries, err := p.loadersFor(ctx, mc)
	if err != nil {
		return Resolved{}, err
	}
	loader, err := fabricPickLoader(entries)
	if err != nil {
		return Resolved{}, newProviderError(CodeInvalidResponse, Fabric, "resolve_loader",
			p.Base()+"/versions/loader/"+mc, err)
	}
	return Resolved{
		Version: mc,
		Pins:    map[string]string{Fabric.DepVar: loader},
		Detail:  "loader " + loader,
	}, nil
}
