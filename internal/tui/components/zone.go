// Este archivo inicializa el gestor global de zonas de ratón (bubblezone) que
// permite hacer clicables los botones y las filas del Home (§4.1). Se inicializa
// al importar el paquete components, de modo que cualquier Mark() sea seguro.
package components

import zone "github.com/lrstanley/bubblezone"

func init() { zone.NewGlobal() }
