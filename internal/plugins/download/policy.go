// Package download implementa la obtenciÃ³n segura de un artefacto de plugin
// (JAR/ZIP): descarga en streaming con el lÃ­mite aplicado DURANTE la
// transferencia, escritura en un archivo temporal dentro del propio directorio
// destino, verificaciÃ³n (tamaÃ±o y checksum SHA-256) y movimiento ATÃ“MICO.
//
// PolÃ­tica por defecto, la mÃ¡s restrictiva:
//
//   - Solo https:// (http:// requiere AllowHTTP explÃ­cito).
//   - Se rechazan loopback, redes privadas, link-local, CGNAT, multicast y
//     rangos reservados/de documentaciÃ³n. AllowLoopback y AllowPrivate son las
//     Ãºnicas excepciones y se documentan como riesgo asumido.
//   - Se valida la IP RESUELTA de cada conexiÃ³n (no solo el hostname de la URL)
//     y cada salto de redirecciÃ³n: cubre SSRF y DNS rebinding.
//   - El nombre final se valida (sin separadores, sin "..", sin caracteres de
//     control) y su extensiÃ³n debe estar en la lista permitida.
//   - Nunca se sobrescribe un archivo existente salvo polÃ­tica explÃ­cita
//     (ConflictRename o ConflictReplace).
//
// Lo que este paquete NO puede garantizar: que el artefacto sea seguro, estÃ©
// libre de cÃ³digo malicioso o sea compatible con el motor. Solo asegura
// transporte controlado, tamaÃ±o acotado, integridad frente a un checksum
// publicado e instalaciÃ³n atÃ³mica.
package download

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/version"
)

// LÃ­mites por defecto. Son deliberadamente conservadores: un plugin o mod
// individual no deberÃ­a acercarse a ellos.
const (
	// DefaultMaxBytes es el lÃ­mite por defecto de un artefacto: 64 MiB.
	DefaultMaxBytes int64 = 64 << 20
	// MaxAllowedBytes es el techo absoluto del lÃ­mite configurable: 1 GiB.
	MaxAllowedBytes int64 = 1 << 30
	// DefaultMaxRedirects es el nÃºmero mÃ¡ximo de redirecciones seguidas.
	DefaultMaxRedirects = 5
	// DefaultTimeout es el tiempo mÃ¡ximo de la descarga completa.
	DefaultTimeout = 2 * time.Minute
	// maxNameLen limita la longitud del nombre final.
	maxNameLen = 120
)

// tempPattern es el patrÃ³n del archivo temporal; su extensiÃ³n .tmp lo mantiene
// invisible para el cargador de plugins del servidor.
const tempPattern = tempPrefix + "*" + tempSuffix

// DefaultExtensions son las extensiones aceptadas para el archivo final.
var DefaultExtensions = []string{".jar", ".zip"}

// Conflict describe quÃ© hacer cuando ya existe un archivo con el nombre final.
type Conflict string

const (
	// ConflictFail aborta con un error explÃ­cito (comportamiento por defecto).
	ConflictFail Conflict = "fail"
	// ConflictRename instala con un sufijo -2, -3â€¦ y lo informa en el resultado.
	ConflictRename Conflict = "rename"
	// ConflictReplace sustituye el archivo existente (solo si se pide).
	ConflictReplace Conflict = "replace"
)

// Policy reÃºne los lÃ­mites y las excepciones de una descarga. Su valor cero es
// vÃ¡lido y equivale a DefaultPolicy.
type Policy struct {
	// MaxBytes es el lÃ­mite de bytes aplicado durante la transferencia.
	// 0 â‡’ DefaultMaxBytes. No puede superar MaxAllowedBytes.
	MaxBytes int64
	// AllowedExtensions son las extensiones aceptadas (por defecto .jar/.zip).
	AllowedExtensions []string
	// AllowHTTP permite http:// sin cifrado. Riesgo asumido y documentado.
	AllowHTTP bool
	// AllowPrivate permite rangos privados, link-local y metadatos de nube.
	AllowPrivate bool
	// AllowLoopback permite loopback (127.0.0.0/8 y ::1). Pensado para
	// pruebas y proxies locales.
	AllowLoopback bool
	// Conflict define quÃ© hacer si el archivo destino ya existe.
	Conflict Conflict
	// MaxRedirects limita los saltos de redirecciÃ³n (0 â‡’ 5).
	MaxRedirects int
	// Timeout es el tiempo mÃ¡ximo de la operaciÃ³n completa (0 â‡’ 2 min).
	Timeout time.Duration
	// UserAgent identifica las peticiones (0 â‡’ "badomens/<versiÃ³n>").
	UserAgent string
}

// DefaultPolicy devuelve la polÃ­tica por defecto.
func DefaultPolicy() Policy {
	return Policy{
		MaxBytes:          DefaultMaxBytes,
		AllowedExtensions: append([]string(nil), DefaultExtensions...),
		Conflict:          ConflictFail,
		MaxRedirects:      DefaultMaxRedirects,
		Timeout:           DefaultTimeout,
		UserAgent:         "badomens/" + version.Version,
	}
}

// withDefaults completa una polÃ­tica con los valores por defecto.
func (p Policy) withDefaults() Policy {
	d := DefaultPolicy()
	if p.MaxBytes <= 0 {
		p.MaxBytes = d.MaxBytes
	}
	if len(p.AllowedExtensions) == 0 {
		p.AllowedExtensions = d.AllowedExtensions
	}
	if p.Conflict == "" {
		p.Conflict = d.Conflict
	}
	if p.MaxRedirects <= 0 {
		p.MaxRedirects = d.MaxRedirects
	}
	if p.Timeout <= 0 {
		p.Timeout = d.Timeout
	}
	if strings.TrimSpace(p.UserAgent) == "" {
		p.UserAgent = d.UserAgent
	}
	return p
}

// Validate comprueba que la polÃ­tica sea coherente antes de usarla.
func (p Policy) Validate() error {
	if p.MaxBytes > MaxAllowedBytes {
		return errx.New(errx.CodeUsage,
			fmt.Sprintf("el lÃ­mite de descarga (%s) supera el mÃ¡ximo permitido (%s)", HumanBytes(p.MaxBytes), HumanBytes(MaxAllowedBytes)),
			"indica un lÃ­mite menor").WithDetail("reason", "limite_invalido")
	}
	switch p.Conflict {
	case ConflictFail, ConflictRename, ConflictReplace:
	default:
		return errx.New(errx.CodeUsage,
			"polÃ­tica de conflicto desconocida: "+string(p.Conflict),
			"usa fail, rename o replace").WithDetail("reason", "conflicto_invalido")
	}
	if len(p.AllowedExtensions) == 0 {
		return errx.New(errx.CodeUsage, "no hay extensiones permitidas", "").WithDetail("reason", "extensiones_invalidas")
	}
	return nil
}

// reservedNets son los rangos no enrutables a Internet que se rechazan por
// defecto aunque no sean "privados" en sentido estricto (RFC 5735, 6598, 3849â€¦).
var reservedNets = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("255.255.255.255/32"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

// localSuffixes son dominios que resuelven a la propia mÃ¡quina o a la red local
// y que nunca deben usarse para descargar cÃ³digo.
var localSuffixes = []string{".localhost", ".local", ".internal", ".home.arpa", ".lan"}

// restriction clasifica una IP: devuelve "" si es pÃºblica, o el motivo por el
// que se bloquea. Las IP IPv4 embebidas en IPv6 se normalizan primero.
func restriction(ip netip.Addr) string {
	ip = ip.Unmap()
	switch {
	case !ip.IsValid():
		return "direcciÃ³n invÃ¡lida"
	case ip.IsUnspecified():
		return "direcciÃ³n no especificada"
	case ip.IsLoopback():
		return "loopback"
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast():
		return "link-local"
	case ip.IsPrivate():
		return "red privada"
	case ip.IsMulticast():
		return "multicast"
	}
	for _, n := range reservedNets {
		if n.Contains(ip) {
			return "rango reservado"
		}
	}
	return ""
}

// allowIP aplica la polÃ­tica a una IP concreta. Se usa dos veces: al validar el
// hostname de la URL (mensaje temprano) y al validar la IP realmente resuelta
// por el dialer (barrera definitiva frente a DNS rebinding).
func (p Policy) allowIP(ip netip.Addr) error {
	if !ip.IsValid() {
		return rejected("", "direcciÃ³n invÃ¡lida")
	}
	ip = ip.Unmap()
	reason := restriction(ip)
	if reason == "" {
		return nil
	}
	if p.AllowPrivate {
		return nil
	}
	if reason == "loopback" && p.AllowLoopback {
		return nil
	}
	return rejected(ip.String(), reason)
}

// rejected construye el error de destino no permitido, con motivo estable en
// `details.reason` para agentes.
func rejected(host, reason string) error {
	shown := host
	if shown == "" {
		shown = "el destino"
	}
	return errx.New(errx.CodeUsage,
		fmt.Sprintf("destino no permitido: %s (%s)", shown, reason),
		"solo se descargan artefactos desde orÃ­genes pÃºblicos por https://; las excepciones --allow-private/--allow-loopback asumen el riesgo de SSRF").
		WithDetail("reason", "destino_no_permitido").
		WithDetail("category", reason).
		WithDetail("destination", shown)
}

// checkURL valida esquema y host de una URL antes de pedirla. Es la primera
// barrera (mensaje claro); la definitiva es el dialer, que valida la IP resuelta.
func (p Policy) checkURL(u *url.URL) error {
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !p.AllowHTTP {
			return errx.New(errx.CodeUsage,
				"la URL usa http:// (sin cifrado)",
				"usa una URL https://; si asumes el riesgo de una descarga sin cifrado, activa la polÃ­tica explÃ­cita --allow-http").
				WithDetail("reason", "tls_requerido")
		}
	default:
		return errx.New(errx.CodeUsage,
			"esquema de URL no soportado: "+u.Scheme,
			"usa una URL https:// que apunte a un .jar o .zip").
			WithDetail("reason", "esquema_no_soportado")
	}
	host := u.Hostname()
	if host == "" {
		return errx.New(errx.CodeUsage, "la URL no indica servidor", "revisa la URL").
			WithDetail("reason", "destino_invalido")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return p.allowIP(ip)
	}
	lower := strings.ToLower(strings.TrimSuffix(host, "."))
	if lower == "localhost" {
		return rejected(host, "loopback")
	}
	for _, suf := range localSuffixes {
		if strings.HasSuffix(lower, suf) {
			return rejected(host, "nombre local")
		}
	}
	return nil
}

// checkRedirect se ejecuta en cada salto: se vuelve a validar esquema y destino,
// y se limita el nÃºmero total de saltos.
func (p Policy) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= p.MaxRedirects {
		return errx.New(errx.CodeUsage,
			fmt.Sprintf("la descarga encadena mÃ¡s de %d redirecciones", p.MaxRedirects),
			"usa la URL final del artefacto en lugar de un acortador").
			WithDetail("reason", "redirecciones_excesivas")
	}
	return p.checkURL(req.URL)
}

// control valida la IP resuelta justo antes de abrir la conexiÃ³n TCP: es la
// pieza que cierra el DNS rebinding (el hostname puede resolver a una IP
// privada despuÃ©s de haber validado la URL).
func (p Policy) control(_ context.Context, network, address string, _ syscall.RawConn) error {
	if !strings.HasPrefix(network, "tcp") {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return errx.New(errx.CodeUsage, "direcciÃ³n de destino ilegible: "+address, "").
			WithDetail("reason", "destino_invalido")
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return errx.New(errx.CodeUsage, "el destino no resolviÃ³ a una IP vÃ¡lida: "+host, "").
			WithDetail("reason", "destino_invalido")
	}
	return p.allowIP(ip)
}

// HTTPClient construye el cliente HTTP con la polÃ­tica aplicada: dialer que
// valida la IP resuelta, validaciÃ³n de cada redirecciÃ³n y tiempo mÃ¡ximo total.
func (p Policy) HTTPClient() *http.Client {
	pol := p.withDefaults()
	dialer := &net.Dialer{
		Timeout:        30 * time.Second,
		KeepAlive:      30 * time.Second,
		ControlContext: pol.control,
	}
	return &http.Client{
		Timeout: pol.Timeout,
		Transport: &http.Transport{
			// Un proxy de entorno se acepta (redes corporativas), pero se
			// valida como destino: con proxy, la IP final la elige el proxy.
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          4,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			ExpectContinueTimeout: time.Second,
		},
		CheckRedirect: pol.checkRedirect,
	}
}

// NormalizeName valida y normaliza el nombre final del artefacto. Rechaza (no
// "sanea"): ante un nombre ambiguo es preferible fallar que adivinar. Sin
// separadores, sin "..", sin caracteres de control, sin nombres reservados de
// Windows y con una extensiÃ³n permitida.
func NormalizeName(raw string, allowedExts []string) (string, error) {
	if len(allowedExts) == 0 {
		allowedExts = DefaultExtensions
	}
	name := raw
	switch {
	case strings.TrimSpace(name) == "":
		return "", invalidName(raw, "el nombre del archivo estÃ¡ vacÃ­o", nil)
	case name != strings.TrimSpace(name):
		return "", invalidName(raw, "el nombre del archivo tiene espacios al principio o al final", nil)
	case name == "." || name == "..":
		return "", invalidName(raw, "el nombre del archivo es una ruta relativa", nil)
	case strings.HasPrefix(name, "."):
		return "", invalidName(raw, "el nombre del archivo no puede empezar por punto", nil)
	case strings.ContainsAny(name, `/\`):
		return "", invalidName(raw, "el nombre del archivo no puede contener separadores de ruta", nil)
	case strings.HasSuffix(name, ".") || strings.HasSuffix(name, " "):
		return "", invalidName(raw, "el nombre del archivo no puede terminar en punto o espacio", nil)
	case len(name) > maxNameLen:
		return "", invalidName(raw, fmt.Sprintf("el nombre del archivo supera %d caracteres", maxNameLen), nil)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", invalidName(raw, "el nombre del archivo contiene caracteres de control", nil)
		}
	}
	ext := strings.ToLower(path.Ext(name))
	ok := false
	for _, e := range allowedExts {
		if ext == strings.ToLower(e) {
			ok = true
			break
		}
	}
	if !ok {
		return "", invalidName(raw,
			"la extensiÃ³n "+ext+" no estÃ¡ permitida",
			map[string]any{"allowed_extensions": allowedExts})
	}
	if windowsReserved(strings.TrimSuffix(name, ext)) {
		return "", invalidName(raw, "el nombre del archivo estÃ¡ reservado por el sistema", nil)
	}
	return name, nil
}

// windowsReserved detecta nombres de dispositivo reservados por Windows.
func windowsReserved(base string) bool {
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(base) == 4 {
		up := strings.ToUpper(base)
		switch up[:3] {
		case "COM", "LPT":
			return up[3] >= '1' && up[3] <= '9'
		}
	}
	return false
}

// invalidName construye el error tipado de nombre rechazado.
func invalidName(raw, message string, extra map[string]any) error {
	e := errx.New(errx.CodeUsage, message, "usa un nombre terminado en .jar o .zip, sin rutas").
		WithDetail("reason", "nombre_invalido")
	if raw != "" {
		e = e.WithDetail("name", raw)
	}
	for k, v := range extra {
		e = e.WithDetail(k, v)
	}
	return e
}

// HumanBytes formatea un tamaÃ±o en bytes de forma legible.
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
