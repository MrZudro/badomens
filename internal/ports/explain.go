package ports

import (
	"regexp"
	"strings"
)

// upErrorRe extrae el puerto de los mensajes de Docker al publicar.
var upErrorRe = regexp.MustCompile(`:(\d{2,5})\b`)

// TranslateUpError traduce un fallo de `docker compose up` por puerto ocupado a
// un Conflict con su puerto, para ofrecer el mismo diálogo amigable que cuando
// se detecta a tiempo (RF-3). Devuelve ok=false si el mensaje no es de puerto.
func TranslateUpError(msg string) (Conflict, bool) {
	if msg == "" {
		return Conflict{}, false
	}
	low := strings.ToLower(msg)
	if !strings.Contains(low, "already allocated") &&
		!strings.Contains(low, "address already in use") &&
		!strings.Contains(low, "bind: address already in use") &&
		!strings.Contains(low, "port is already in use") {
		return Conflict{}, false
	}
	port := 0
	if m := upErrorRe.FindStringSubmatch(msg); len(m) == 2 {
		port = atoi(m[1])
	}
	detail := strings.TrimSpace(msg)
	if i := strings.IndexByte(detail, '\n'); i >= 0 {
		detail = strings.TrimSpace(detail[:i])
	}
	return Conflict{
		Port:     port,
		Severity: SeverityBlock,
		Owner: Owner{
			Kind:   OwnerUnknown,
			Detail: "Docker no pudo publicar el puerto: " + detail,
		},
	}, true
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}
