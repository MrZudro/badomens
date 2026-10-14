// Package mcuuid calcula el UUID offline de Minecraft (versión 3) que el
// servidor asigna a un jugador cuando ONLINE_MODE=false.
//
// El algoritmo es el del propio servidor: md5("OfflinePlayer:"+nombre) con los
// bits de versión (3) y de variante (RFC 4122) reescritos. Es determinista y no
// consulta la red, así que puede calcularse antes de arrancar la instancia.
package mcuuid

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strings"
)

// Offline devuelve el UUID offline (versión 3) del nombre indicado, en
// minúsculas y con el formato canónico xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx.
//
// Un nombre vacío (o solo espacios) devuelve "" —no existe jugador offline sin
// nombre— para que quien construya listas no escriba un UUID vacío.
func Offline(name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	sum := md5.Sum([]byte("OfflinePlayer:" + name))
	sum[6] = (sum[6] & 0x0F) | 0x30 // versión 3 (MD5 + nombre)
	sum[8] = (sum[8] & 0x3F) | 0x80 // variante RFC 4122 (10xx)
	h := hex.EncodeToString(sum[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}
