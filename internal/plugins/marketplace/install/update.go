package install

import (
	"context"
	"strings"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/plugins"
	"github.com/MrZudro/badomens/internal/plugins/marketplace"
	"github.com/MrZudro/badomens/internal/plugins/marketplace/resolve"
)

// Estados de la comprobación de actualizaciones.
const (
	// UpdateCurrent: la versión instalada sigue siendo la más reciente
	// compatible.
	UpdateCurrent = "al_dia"
	// UpdateAvailable: hay una versión nueva compatible. NO se instala:
	// comprobar actualizaciones nunca modifica el archivo instalado.
	UpdateAvailable = "actualizacion_disponible"
	// UpdateUnknown: no se puede saber (sin procedencia o sin versión
	// registrada).
	UpdateUnknown = "desconocida"
	// UpdateBlocked: la comprobación falló (proveedor caído, resolución
	// incompleta, marketplace no registrado).
	UpdateBlocked = "bloqueada"
)

// UpdateRequest describe una comprobación de actualizaciones.
type UpdateRequest struct {
	// Spec es la instancia a comprobar.
	Spec *instance.Spec
	// Channels son los canales aceptados (vacío ⇒ solo release).
	Channels []string
	// AllowPrerelease acepta canales preliminares al buscar la versión nueva.
	AllowPrerelease bool
	// PersistTargets guarda en el registro de procedencia la versión objetivo
	// detectada. Nunca instala nada: solo anota qué versión hay publicada.
	PersistTargets bool
	// VersionLimit acota cuántas versiones se piden al proveedor.
	VersionLimit int
}

// UpdateCheck es el resultado de comprobar una instalación.
type UpdateCheck struct {
	// Name es el archivo instalado.
	Name string `json:"name"`
	// Version es la versión INSTALADA registrada (puede estar vacía).
	Version string `json:"version,omitempty"`
	// TargetVersion es la versión publicada más reciente compatible.
	TargetVersion string `json:"target_version,omitempty"`
	// TargetVersionID identifica esa versión en el marketplace.
	TargetVersionID string `json:"target_version_id,omitempty"`
	// TargetChannel es el canal de esa versión.
	TargetChannel string `json:"target_channel,omitempty"`
	// Status es uno de los valores Update*.
	Status string `json:"status"`
	// Compatibility es la compatibilidad declarada de la versión publicada.
	Compatibility marketplace.Compatibility `json:"compatibility"`
	// Publication describe el estado de publicación de esa versión.
	Publication string `json:"publication,omitempty"`
	// Detail explica el estado.
	Detail string `json:"detail,omitempty"`
	// Reason es el motivo estable cuando la comprobación está bloqueada.
	Reason string `json:"reason,omitempty"`
	// Warnings son avisos de la resolución (por ejemplo, sin checksum).
	Warnings []string `json:"warnings,omitempty"`
}

// CheckUpdates comprueba, para cada plugin/mod con procedencia registrada, si el
// marketplace publica una versión más reciente compatible.
//
// Garantías:
//
//   - NO descarga ni instala nada: solo consulta catálogo y compara versiones.
//   - NO modifica el archivo instalado ni su checksum.
//   - Si PersistTargets está activo, anota la versión objetivo en el registro de
//     procedencia (un dato, no una instalación).
//   - Los fallos por elemento se devuelven como estado "bloqueada" con el
//     motivo; solo un fallo del propio registro es un error de la llamada.
func (c *Client) CheckUpdates(ctx context.Context, up UpdateRequest) ([]UpdateCheck, error) {
	if up.Spec == nil {
		return nil, errx.New(errx.CodeUsage, "falta la instancia a comprobar",
			"indica la instancia con --instance <slug>").WithDetail("reason", "instancia_ausente")
	}
	reg, err := c.loadRegister(up.Spec)
	if err != nil {
		return nil, err
	}
	records := reg.Installed(up.Spec.Plugins)
	out := make([]UpdateCheck, 0, len(records))

	dirty := false
	for _, rec := range records {
		check := c.checkOne(ctx, up, rec)
		if up.PersistTargets && check.Status == UpdateAvailable {
			updated := rec
			updated.TargetVersion = check.TargetVersion
			reg.Upsert(updated)
			dirty = true
		}
		out = append(out, check)
	}
	if dirty && c.Provenance != nil {
		if err := c.Provenance.Save(up.Spec, reg); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// checkOne comprueba una única instalación.
func (c *Client) checkOne(ctx context.Context, up UpdateRequest, rec plugins.Record) UpdateCheck {
	check := UpdateCheck{Name: rec.Name, Version: rec.Version, Status: UpdateUnknown}
	if !rec.Origin.Known() {
		check.Detail = "sin procedencia registrada: no se sabe de qué proyecto salió"
		return check
	}
	src, err := c.source(marketplace.SourceID(rec.Origin.Source))
	if err != nil {
		check.Status = UpdateBlocked
		check.Reason = reasonOf(err)
		check.Detail = messageOf(err)
		return check
	}
	project := firstNonEmpty(rec.Origin.ProjectSlug, rec.Origin.ProjectID)
	if project == "" {
		check.Status = UpdateBlocked
		check.Detail = "el registro no identifica el proyecto de origen"
		return check
	}
	loader := firstNonEmpty(rec.Loader, up.Spec.Engine)
	mc := firstNonEmpty(rec.GameVersion, up.Spec.Version)

	res, rerr := resolve.Resolve(ctx, resolve.Request{
		Source:           src,
		Project:          project,
		Env:              marketplace.Environment{Engine: up.Spec.Engine, Loader: loader, GameVersion: mc},
		Channels:         up.Channels,
		AllowPrerelease:  up.AllowPrerelease,
		AllowUnverified:  true, // comprobar no instala: un artefacto sin checksum se reporta, no se rechaza
		AllowMissingDeps: true, // las dependencias se avisan, no bloquean una comprobación
		Installed:        nil,
		VersionLimit:     up.VersionLimit,
	})
	if rerr != nil {
		check.Status = UpdateBlocked
		check.Compatibility = marketplace.Unknown
		check.Reason = reasonOf(rerr)
		check.Detail = messageOf(rerr)
		if res != nil {
			check.Publication = res.Publication
			check.Warnings = res.Warnings
		}
		return check
	}
	check.Compatibility = res.Compat
	check.Publication = res.Publication
	check.Warnings = res.Warnings

	switch {
	case strings.TrimSpace(rec.Version) == "":
		check.TargetVersion = res.Version.Number
		check.TargetVersionID = res.Version.ID
		check.TargetChannel = res.Version.Channel
		check.Detail = "no se registró la versión instalada; la más reciente compatible es " + res.Version.Number
		return check
	case sameVersion(rec, res.Version):
		check.Status = UpdateCurrent
		check.Detail = "la versión instalada " + rec.Version + " sigue siendo la más reciente compatible"
		return check
	default:
		check.Status = UpdateAvailable
		check.TargetVersion = res.Version.Number
		check.TargetVersionID = res.Version.ID
		check.TargetChannel = res.Version.Channel
		check.Detail = "hay una versión más reciente compatible: " + res.Version.Number +
			" (instalada: " + rec.Version + "); no se instala automáticamente"
		return check
	}
}

// sameVersion compara la versión instalada con la resuelta.
func sameVersion(rec plugins.Record, v marketplace.Version) bool {
	installed := strings.TrimSpace(rec.Version)
	if installed == "" {
		return false
	}
	if strings.EqualFold(installed, strings.TrimSpace(v.Number)) {
		return true
	}
	// El registro puede haber guardado el identificador en lugar del número
	// (por ejemplo, si el proveedor renumeró la versión).
	if id := strings.TrimSpace(rec.Origin.VersionID); id != "" {
		return strings.EqualFold(id, strings.TrimSpace(v.ID))
	}
	return false
}

// loadRegister lee el registro de procedencia; sin registro devuelve uno vacío.
func (c *Client) loadRegister(spec *instance.Spec) (*plugins.Register, error) {
	if c.Provenance == nil {
		return plugins.NewRegister(), nil
	}
	return c.Provenance.Load(spec)
}

// firstNonEmpty devuelve el primer valor no vacío.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// messageOf extrae el mensaje legible de un error tipado.
func messageOf(err error) string {
	_, msg, _ := errx.From(err)
	return msg
}

// reasonOf extrae el motivo estable de un error tipado.
func reasonOf(err error) string {
	if d := errx.Details(err); d != nil {
		if r, ok := d["reason"].(string); ok {
			return r
		}
	}
	return ""
}
