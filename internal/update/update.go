// Package update comprueba si hay una versión nueva publicada en GitHub.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/version"
)

// Info describe el resultado de la comprobación de actualización.
type Info struct {
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"update_available"`
	URL             string `json:"url,omitempty"`
	PublishedAt     string `json:"published_at,omitempty"`
}

// httpClient permite inyectar un cliente en pruebas.
var httpClient = &http.Client{Timeout: 10 * time.Second}

// Check consulta la última release del repositorio (slug owner/repo) y la
// compara con la versión actual del binario.
func Check(ctx context.Context, repo string) (Info, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" || !strings.Contains(repo, "/") {
		return Info{}, errx.Usage(
			"repositorio inválido: "+repo,
			"configura 'repo: owner/nombre' en config.yaml",
		)
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Info{}, errx.Wrap(errx.CodeGeneral, err, "no se pudo preparar la consulta de actualización", "")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "badomens/"+version.Version)

	resp, err := httpClient.Do(req)
	if err != nil {
		return Info{}, errx.Wrap(errx.CodeUnavailable, err, "no se pudo consultar GitHub", "comprueba tu conexión")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return Info{}, errx.NotFound("el repositorio "+repo+" no tiene releases publicadas", "")
	}
	if resp.StatusCode != http.StatusOK {
		return Info{}, errx.Unavailable(fmt.Sprintf("GitHub respondió %d", resp.StatusCode), "reintenta más tarde")
	}

	var payload struct {
		TagName     string `json:"tag_name"`
		HTMLURL     string `json:"html_url"`
		PublishedAt string `json:"published_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Info{}, errx.Wrap(errx.CodeGeneral, err, "respuesta de GitHub ilegible", "")
	}

	latest := strings.TrimPrefix(payload.TagName, "v")
	info := Info{
		Current:     version.Version,
		Latest:      latest,
		URL:         payload.HTMLURL,
		PublishedAt: payload.PublishedAt,
	}
	info.UpdateAvailable = newer(latest, version.Version)
	return info, nil
}

// newer indica si candidate es una versión posterior a current (comparación
// semántica laxa: numérica por componentes y con sufijos dev considerados
// anteriores a cualquier release estable).
func newer(candidate, current string) bool {
	c := parseSemver(candidate)
	cur := parseSemver(current)
	for i := 0; i < 3; i++ {
		if c.nums[i] != cur.nums[i] {
			return c.nums[i] > cur.nums[i]
		}
	}
	// Misma base numérica: un prerelease (dev/rc) es anterior a lo estable.
	if cur.pre != "" && c.pre == "" {
		return true
	}
	return false
}

type semver struct {
	nums [3]int
	pre  string
}

func parseSemver(s string) semver {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		pre := s[i+1:]
		s = s[:i]
		return semver{nums: splitNums(s), pre: pre}
	}
	return semver{nums: splitNums(s)}
}

func splitNums(s string) [3]int {
	var out [3]int
	for i, part := range strings.SplitN(s, ".", 3) {
		if i > 2 {
			break
		}
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			n = 0
		}
		out[i] = n
	}
	return out
}
