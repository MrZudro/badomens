// Package update comprueba si hay una versión nueva publicada en GitHub y, con
// Apply, la descarga verificando el checksum. No depende de `/releases/latest`
// (que ignora las pre-releases): consulta `releases?per_page=1` para ver la
// última release publicada sea cual sea su estado.
package update

import (
	"context"
	"strconv"
	"strings"

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

// Check consulta la última release publicada del repositorio (slug owner/repo)
// y la compara con la versión actual del binario. Usa el endpoint de listado de
// releases (no `/releases/latest`), porque este proyecto publica pre-releases y
// GitHub ignoraría la más reciente.
func Check(ctx context.Context, repo string) (Info, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" || !strings.Contains(repo, "/") {
		return Info{}, errx.Usage(
			"repositorio inválido: "+repo,
			"configura 'repo: owner/nombre' en config.yaml",
		)
	}
	rel, err := latestRelease(ctx, repo)
	if err != nil {
		return Info{}, err
	}
	latest := strings.TrimPrefix(rel.TagName, "v")
	info := Info{
		Current:     version.Version,
		Latest:      latest,
		URL:         rel.HTMLURL,
		PublishedAt: rel.PublishedAt,
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
