// Command badomens es el punto de entrada del binario.
package main

import (
	"os"

	"github.com/MrZudro/badomens/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
