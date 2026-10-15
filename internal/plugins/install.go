package plugins

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/plugins/archive"
	"github.com/MrZudro/badomens/internal/plugins/download"
)

// InstallOptions reÃºne la polÃ­tica de instalaciÃ³n. Su valor cero aplica las
// polÃ­ticas por defecto, que son las mÃ¡s restrictivas.
type InstallOptions struct {
	// Download reÃºne los lÃ­mites de red y de archivo (lÃ­mite de tamaÃ±o, https
	// obligatorio, anti-SSRF, polÃ­tica de conflicto de nombresâ€¦).
	Download download.Policy
	// Archive reÃºne los lÃ­mites al inspeccionar la estructura del JAR/ZIP.
	Archive archive.Limits
	// SHA256 es el checksum esperado en hexadecimal (opcional). Si el origen lo
	// publica, pasarlo aquÃ­: sin checksum, un artefacto vÃ¡lido sigue siendo un
	// artefacto de origen no verificado.
	SHA256 string
	// Name fuerza el nombre final (Ãºtil cuando la URL no termina en .jar/.zip).
	Name string
}

// Installed describe una instalaciÃ³n completada y verificada.
type Installed struct {
	// Name es el nombre final (con sufijo si hubo conflicto y se renombrÃ³).
	Name string `json:"name"`
	// Path es la ruta final del archivo instalado.
	Path string `json:"path"`
	// Bytes es el tamaÃ±o instalado.
	Bytes int64 `json:"bytes"`
	// SHA256 es el checksum calculado durante la instalaciÃ³n.
	SHA256 string `json:"sha256"`
	// Source es la URL o la ruta local de la que se obtuvo.
	Source string `json:"source"`
	// Archive resume la inspecciÃ³n estructural del JAR/ZIP.
	Archive archive.Report `json:"archive"`
	// ChecksumVerified indica si se verificÃ³ un SHA-256 aportado por el usuario.
	ChecksumVerified bool `json:"checksum_verified"`
	// Replaced indica si se sustituyÃ³ un archivo existente.
	Replaced bool `json:"replaced,omitempty"`
	// RenamedFrom es el nombre original cuando se renombrÃ³ por conflicto.
	RenamedFrom string `json:"renamed_from,omitempty"`
}

// ThirdPartyWarning es el aviso obligatorio sobre cÃ³digo de terceros. No
// promete ningÃºn anÃ¡lisis de seguridad: lo Ãºnico verificable es que el archivo
// tiene estructura de JAR/ZIP y respeta los lÃ­mites de recursos.
func ThirdPartyWarning() string {
	return "AVISO: un plugin/mod es cÃ³digo de terceros que se ejecutarÃ¡ con los permisos del servidor. " +
		"Un JAR estructuralmente vÃ¡lido NO implica que sea seguro ni compatible: se comprueba la forma del archivo, " +
		"nunca su contenido. Instala solo artefactos de fuentes en las que confÃ­es y revisa origen y licencia."
}

// Install instala un plugin/mod desde una URL https:// o desde un archivo
// local, siguiendo el flujo: descarga/copia en streaming a un temporal del
// directorio destino â†’ verificaciÃ³n (tamaÃ±o, checksum, estructura JAR/ZIP) â†’
// movimiento atÃ³mico. Nada queda en el destino hasta que todo ha ido bien, y
// nunca se sobrescribe un archivo existente sin polÃ­tica explÃ­cita.
//
// La actualizaciÃ³n del registro persistente (spec.Plugins) NO ocurre aquÃ­: la
// hace el llamador solo despuÃ©s de que Install devuelva Ã©xito.
func (i *Installer) Install(ctx context.Context, spec *instance.Spec, source string, opts InstallOptions) (*Installed, error) {
	if spec == nil {
		return nil, errx.New(errx.CodeUsage, "falta la instancia destino", "indica la instancia con --instance <slug>")
	}
	if spec.Engine == "vanilla" {
		return nil, errx.Usage("vanilla no admite plugins ni mods",
			"usa un motor con soporte (paper, fabric, forge, neoforge)")
	}
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, errx.New(errx.CodeUsage, "falta el archivo o la URL del plugin", "indica un .jar/.zip o una URL https://").
			WithDetail("reason", "origen_vacio")
	}

	dir := i.Dir(spec)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, errx.Wrap(errx.CodeGeneral, err, "no se pudo preparar la carpeta "+dir, "revisa permisos")
	}
	// Best-effort: retira temporales huérfanos de una ejecución interrumpida.
	// Solo borra los propios y con más de una hora, para no tocar una descarga
	// en curso. El servidor nunca los cargaría (no terminan en .jar).
	if _, cerr := download.CleanStaleTemps(dir, time.Hour); cerr != nil {
		return nil, cerr
	}

	pol := opts.Download
	if strings.TrimSpace(pol.UserAgent) == "" {
		pol.UserAgent = engines.DefaultUserAgent()
	}
	req := download.Request{
		DestDir: dir,
		Name:    opts.Name,
		SHA256:  strings.TrimSpace(opts.SHA256),
		Policy:  pol,
	}

	var (
		staged *download.Staged
		err    error
	)
	if remote := schemeOf(source); remote != "" {
		if remote != "http" && remote != "https" {
			return nil, errx.New(errx.CodeUsage, "esquema no soportado: "+remote+"://",
				"usa una URL https:// o un archivo local").WithDetail("reason", "esquema_no_soportado")
		}
		req.URL = source
		staged, err = download.StageURL(ctx, req)
	} else {
		staged, err = download.StageFile(source, req)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = staged.Discard() }() // tras Commit es una operaciÃ³n sin efecto

	rep, err := archive.Validate(staged.Path(), staged.Bytes(), opts.Archive)
	if err != nil {
		return nil, err
	}

	res, err := staged.Commit()
	if err != nil {
		return nil, err
	}
	return &Installed{
		Name:             res.Name,
		Path:             res.Path,
		Bytes:            res.Bytes,
		SHA256:           res.SHA256,
		Source:           res.Source,
		Archive:          rep,
		ChecksumVerified: req.SHA256 != "",
		Replaced:         res.Replaced,
		RenamedFrom:      res.RenamedFrom,
	}, nil
}

// Add instala con la polÃ­tica por defecto y devuelve el nombre final. Se
// mantiene para los llamadores existentes (provision, setup).
func (i *Installer) Add(ctx context.Context, spec *instance.Spec, source string) (string, error) {
	res, err := i.Install(ctx, spec, source, InstallOptions{})
	if err != nil {
		return "", err
	}
	return res.Name, nil
}

// schemeOf devuelve el esquema de la fuente ("http", "https"â€¦) o "" si es una
// ruta local. Solo se reconoce la forma `esquema://`, que una ruta local nunca
// tiene (ni siquiera en Windows, donde el separador es `\`).
func schemeOf(source string) string {
	idx := strings.Index(source, "://")
	if idx <= 0 {
		return ""
	}
	return strings.ToLower(source[:idx])
}
