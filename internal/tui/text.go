// Package tui implementa el panel interactivo multi-instancia (F4). Es un
// cliente del propio binario: importa directamente los paquetes internos
// (config, instance, runtime, backup, tps) y no usa el contrato JSON, que queda
// reservado para el CLI no interactivo (Emiria/Jamcord, scripts, MCP).
package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ansiRe elimina secuencias de escape de terminal: CSI (ESC [ …), OSC (ESC ] …)
// , ESC ( … ) y ESC simple de 1 char.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07]*(?:\x07|\x1b\\)|\x1b\([0-9A-Za-z]|\x1b[XM]`)

// stripANSI quita secuencias de escape y caracteres de control (C0) del texto
// tal cual llega de docker, dejando solo texto legible.
func stripANSI(s string) string {
	s = ansiRe.ReplaceAllString(s, "")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

// shortSha recorta un sha256 a sus primeros 12 caracteres para mostrarlo en el
// panel; si es más corto lo devuelve tal cual.
func shortSha(sha string) string {
	if len(sha) <= 12 {
		return sha
	}
	return sha[:12]
}

// humanBytes formatea un tamaño en bytes con unidad binaria (B, KB, MB…).
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// intValue convierte un texto a entero no negativo. El texto vacío es válido y
// equivale a 0; un valor negativo o no numérico se rechaza.
func intValue(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
