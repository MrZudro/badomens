// Package version centraliza la versión del binario. Se inyecta en tiempo de
// compilación con GoReleaser (-ldflags) y tiene un valor por defecto para
// compilaciones desde el código.
package version

// Variables inyectadas en tiempo de compilación.
var (
	// Version es la versión semántica del binario.
	Version = "0.0.0-dev"
	// Commit es el hash corto del commit con el que se compiló.
	Commit = "none"
	// Date es la fecha de compilación en formato RFC3339.
	Date = "unknown"
)

// String devuelve una cadena humana con la versión, el commit y la fecha.
func String() string {
	return Version + " (commit " + Commit + ", " + Date + ")"
}
