package tui

import (
	"encoding/base64"
	"os"

	"github.com/atotto/clipboard"
)

// copyToClipboard copia texto al portapapeles. Primero intenta el portapapeles
// del sistema (atotto/clipboard) y, si no está disponible (p. ej. por SSH sin
// X11), usa la secuencia OSC 52, que funciona a través de muchos terminales.
func copyToClipboard(text string) error {
	if err := clipboard.WriteAll(text); err == nil {
		return nil
	}
	return osc52Copy(text)
}

// osc52Copy escribe la secuencia OSC 52 en la salida estándar.
func osc52Copy(text string) error {
	enc := base64.StdEncoding.EncodeToString([]byte(text))
	seq := "\x1b]52;c;" + enc + "\a"
	_, err := os.Stdout.WriteString(seq)
	return err
}
