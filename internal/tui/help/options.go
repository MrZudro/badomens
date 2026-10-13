// Package help centraliza las descripciones de las opciones de una instancia
// (RF-4): el mismo registro alimenta el asistente (wizard), la pestaña Ajustes,
// `badomens setup --help`, `badomens setup --explain <campo>` y la paleta de
// descripciones de la TUI. Un test recorre el catálogo y falla si falta la
// descripción corta (≤ 90 caracteres) o la larga.
package help

import "sort"

// Tag clasifica el impacto de una opción.
type Tag string

// Etiquetas de impacto.
const (
	TagRequiresRestart    Tag = "RequiresRestart"
	TagOnlyAtCreation     Tag = "OnlyAtCreation"
	TagAffectsPerformance Tag = "AffectsPerformance"
	TagAffectsSecurity    Tag = "AffectsSecurity"
	TagAffectsPlayers     Tag = "AffectsPlayers"
)

// Help describe una opción del asistente/ajustes.
type Help struct {
	// Key es el nombre de campo (igual al de provision.Request).
	Key string
	// Title es el título mostrado.
	Title string
	// Short es la descripción corta (≤ 90 caracteres), siempre visible.
	Short string
	// Long es la descripción larga (párrafo llano), visible bajo demanda.
	Long string
	// Example es un ejemplo de valor opcional.
	Example string
	// Impact son las etiquetas de impacto.
	Impact []Tag
	// Default explica el valor por defecto y por qué.
	Default string
}

// Entries es el catálogo en orden de inserción.
var Entries = []Help{
	{Key: "name", Title: "Nombre", Short: "Identifica tu servidor; se usa para su carpeta y contenedor (solo minúsculas, números y guiones).", Long: "El nombre es la cara visible del servidor y lo que verán los demás. Se convierte en el identificador técnico (slug) que nombra la carpeta, el contenedor y los backup: por eso solo admite minúsculas, números y guiones. No se puede cambiar después de crear la instancia.", Example: "survival-amigos", Impact: []Tag{TagOnlyAtCreation}, Default: "obligatorio"},
	{Key: "engine", Title: "Motor", Short: "El programa que ejecuta el servidor; define si puedes usar plugins o mods.", Long: "El motor es el software que corre dentro del contenedor. Paper es el más recomendado para jugar sin mods: es rápido y optimizado. Fabric, Forge y NeoForge sirven para instalar mods, pero los jugadores también deben instalarlos. Vanilla es el servidor oficial de Mojang, sin plugins ni mods. Solo se elige al crear.", Example: "paper", Impact: []Tag{TagOnlyAtCreation}},
	{Key: "engine.paper", Title: "Paper", Short: "Rápido y optimizado, con plugins. Ideal para jugar con amigos sin mods.", Long: "Paper es un servidor de alto rendimiento basado en el de Mojang, con compatibilidad con plugins. Es la opción por defecto: los jugadores entran con el Minecraft normal, sin instalar nada.", Impact: nil},
	{Key: "engine.fabric", Title: "Fabric", Short: "Ligero y moderno para mods. Los jugadores también necesitan Fabric y los mismos mods.", Long: "Fabric es un cargador de mods ligero y de arranque rápido. Cada jugador debe instalar el mismo Fabric Loader y las mismas mods para entrar. Buena elección para modpacks centrados en rendimiento.", Impact: nil},
	{Key: "engine.forge", Title: "Forge", Short: "Clásico para modpacks grandes. Los jugadores necesitan Forge y los mismos mods.", Long: "Forge es el cargador de mods más veterano y el que soporta la mayoría de los modpacks grandes. Los jugadores instalan Forge y las mods del servidor.", Impact: nil},
	{Key: "engine.neoforge", Title: "NeoForge", Short: "Sucesor de Forge para mods recientes. Los jugadores necesitan NeoForge.", Long: "NeoForge es el sucesor comunitario de Forge, compatible con las versiones recientes de Minecraft. Los jugadores instalan NeoForge y las mods del servidor.", Impact: nil},
	{Key: "engine.vanilla", Title: "Vanilla", Short: "El servidor oficial de Mojang: sin plugins ni mods; cualquier cliente normal entra.", Long: "Vanilla es el servidor sin modificar de Mojang: cualquier cliente entra sin instalar nada, pero no puedes poner plugins ni mods.", Impact: nil},
	{Key: "version", Title: "Versión de Minecraft", Short: "Los jugadores deben usar esta versión (o una compatible). La marcada ★ es la recomendada.", Long: "La versión fija qué clientes pueden entrar: normalmente los jugadores deben usar exactamente esa versión (u otra compatible). La lista solo muestra versiones que ese motor publica de verdad; las que existen pero no se pueden usar aparecen atenuadas con su motivo. La marcada ★ es la recomendada. Usa la tecla 's' para ver snapshots. Cambiarla en una instancia existente puede corromper el mundo: requiere backup previo.", Example: "1.21.4", Impact: []Tag{TagOnlyAtCreation}},
	{Key: "memory", Title: "Memoria (RAM)", Short: "RAM máxima del servidor. Más no siempre es mejor; te sugerimos una según tu equipo.", Long: "La RAM es el máximo que puede usar la máquina virtual de Java. Darla de más a un servidor quita memoria a otros y no siempre mejora el rendimiento; darla de menos provoca cierres por falta de memoria. La sugerencia sale de la RAM de tu equipo y de cuántos servidores tienes corriendo.", Example: "4G", Impact: []Tag{TagRequiresRestart, TagAffectsPerformance}, Default: "recomendada según el equipo"},
	{Key: "max_players", Title: "Jugadores máximos", Short: "Cuántos pueden estar conectados a la vez. Más jugadores = más CPU y RAM.", Long: "Límite de jugadores simultáneos. Subirlo aumenta la carga de CPU y RAM; el valor recomendado se calcula con la memoria asignada.", Example: "20", Impact: []Tag{TagAffectsPerformance}},
	{Key: "gamemode", Title: "Modo de juego", Short: "Survival, Creativo, Aventura o Espectador para quienes entran por primera vez.", Long: "El modo de juego con el que entran los jugadores nuevos: Survival (sobrevivir), Creativo (construir sin límites), Aventura (respetar reglas del mapa) o Espectador (solo mirar). Los operadores pueden cambiarlo dentro del juego.", Impact: nil},
	{Key: "difficulty", Title: "Dificultad", Short: "Pacífico, Fácil, Normal o Difícil: cuánto daño hacen los monstruos y el hambre.", Long: "La dificultad controla el daño de monstruos y el hambre. Pacífico no genera monstruos hostiles. Difícil es la más exigente.", Impact: nil},
	{Key: "hardcore", Title: "Hardcore", Short: "Si mueres, quedas baneado de ese mundo. Dificultad fija en Difícil.", Long: "En Hardcore, morir expulsa y banea del mundo (el mundo pasa a espectador). La dificultad queda fija en Difícil. No se puede quitar después de crear el mundo.", Impact: []Tag{TagOnlyAtCreation}},
	{Key: "pvp", Title: "PvP", Short: "Permite que los jugadores se hagan daño entre sí.", Long: "El jugador contra jugador (PvP) permite atacar a otros jugadores. Desactivarlo evita el daño entre jugadores dentro del juego.", Impact: nil},
	{Key: "online_mode", Title: "Cuentas premium (modo online)", Short: "Activo: exige cuenta oficial de Minecraft. Inactivo: entra cualquiera con cualquier nombre.", Long: "Con el modo online activo solo entran cuentas oficiales de Minecraft. Con el modo inactivo (crackeado) entra cualquiera con cualquier nombre, lo que permite suplantar a un operador si no activas la whitelist: por eso badomens te lo avisa. El cambio requiere reiniciar.", Impact: []Tag{TagAffectsSecurity, TagRequiresRestart}},
	{Key: "whitelist", Title: "Lista blanca", Short: "Solo pueden entrar los jugadores que tú aprobaste. Muy recomendada si el modo online está inactivo.", Long: "Con la lista blanca activa, solo entran los nombres que añadiste. Es imprescindible si el modo online está desactivado, para impedir suplantaciones.", Impact: []Tag{TagAffectsSecurity}},
	{Key: "ops", Title: "Operadores", Short: "Jugadores con poder de administrador dentro del juego (comandos, modo creativo).", Long: "Los operadores (ops) pueden ejecutar comandos de administración, cambiar el modo de juego y gestionar el servidor desde dentro. Concede op solo a gente de confianza.", Impact: []Tag{TagAffectsSecurity}},
	{Key: "world_type", Title: "Tipo de mundo", Short: "Normal, plano (superflat), biomas grandes o amplificado.", Long: "Define la generación del mundo al crearlo: Normal, Plano (superflat), Biomas grandes (más amplios) o Amplificado (montañas enormes). Solo afecta a mundos nuevos.", Example: "normal", Impact: []Tag{TagOnlyAtCreation}},
	{Key: "seed", Title: "Semilla", Short: "Texto/número que genera el mapa; la misma semilla da el mismo mundo. Vacío = aleatoria.", Long: "La semilla fija la generación del terreno: con la misma semilla obtienes el mismo mundo. Déjala vacía para un mundo aleatorio. Solo aplica al crear el mundo.", Example: "-2844874353865282585", Impact: []Tag{TagOnlyAtCreation}},
	{Key: "world_import", Title: "Importar mundo", Short: "Usa un mundo guardado en tu biblioteca en vez de crear uno nuevo.", Long: "En lugar de generar un mundo desde cero, usa un mundo de la biblioteca de badomens (ver 'badomens world list'). El mundo original no se modifica.", Impact: []Tag{TagOnlyAtCreation}},
	{Key: "view_distance", Title: "Distancia de renderizado", Short: "Cuántos chunks ve cada jugador. Más alto se ve mejor pero consume más.", Long: "Chunks visibles para cada jugador. Subirla mejora la vista pero consume CPU y RAM del servidor. El valor recomendado se ajusta a tu equipo.", Example: "10", Impact: []Tag{TagAffectsPerformance}},
	{Key: "simulation_distance", Title: "Distancia de simulación", Short: "Hasta dónde se mueven mobs y cultivos. Bajarla mejora mucho el rendimiento.", Long: "Distancia en la que el servidor simula entidades, cultivos y mobs. Es la opción que más afecta al rendimiento con muchos jugadores: bajarla suele arreglar tirones sin que se note casi.", Example: "6", Impact: []Tag{TagAffectsPerformance}},
	{Key: "jvm_preset", Title: "Ajustes de Java", Short: "\"Equilibrado\" (recomendado), \"Ligero\" para poca RAM o \"Personalizado\" para expertos.", Long: "Conjunto de ajustes de la máquina virtual: Equilibrado (recomendado), Ligero (para servidores con poca RAM) o Personalizado (flags propias de Java). Cambiarlo requiere reiniciar.", Impact: []Tag{TagRequiresRestart, TagAffectsPerformance}},
	{Key: "port", Title: "Puerto", Short: "Número por el que se conectan los jugadores. Lo elegimos libre y lo comprobamos por ti.", Long: "El puerto es la dirección numérica de conexión (por defecto 25565). badomens comprueba que esté libre en tu equipo, en Docker y en la tailnet y propone el primero libre si no lo está. Cambiarlo requiere reiniciar.", Example: "25565", Impact: []Tag{TagRequiresRestart}},
	{Key: "motd", Title: "Mensaje del servidor (MOTD)", Short: "Texto que ven los jugadores en la lista de servidores.", Long: "El MOTD (Message Of The Day) es la línea de descripción que se ve bajo el nombre del servidor en Minecraft. Acepta texto plano; los códigos de color se insertan como &a, &b…", Example: "¡Bienvenidos a Survival!", Impact: nil},
	{Key: "idle", Title: "Apagado por inactividad", Short: "Apaga el servidor solo cuando nadie está conectado durante el tiempo elegido. Ahorra CPU y RAM.", Long: "Si no hay jugadores conectados durante el tiempo que elijas, el servidor se apaga solo (guardando el mundo). Cualquier entrada resetea el contador. Muy útil para no gastar recursos cuando no se juega.", Example: "15m", Impact: nil, Default: "deshabilitado por defecto"},
	{Key: "backups.schedule", Title: "Backups automáticos", Short: "Copias de seguridad con checksum cada cierto tiempo, con retención configurable.", Long: "Programa copias de seguridad automáticas (diarias, semanales o cada N horas) con verificación de integridad y número de copias que conservar. Restaurar siempre crea un backup previo.", Impact: nil},
	{Key: "autostart", Title: "Arranque automático", Short: "Inicia este servidor cuando se enciende el equipo o Docker.", Long: "Con el arranque automático, el contenedor se levanta al arrancar el equipo (restart: unless-stopped de Compose), de modo que el servidor vuelve a estar en línea sin intervención.", Impact: nil},
	{Key: "allow_flight", Title: "Permitir volar", Short: "Evita expulsar a quienes \"vuelan\" por mods o elytra. Desactivarlo protege contra trampas.", Long: "Minecraft expulsa por \"vuelo no permitido\" a los jugadores que vuelan sin permiso (con elytra, mods o por lag). Actívalo si usas mods que permiten volar; desactívalo para frenar trampas.", Impact: []Tag{TagAffectsSecurity}},
	{Key: "command_blocks", Title: "Bloques de comandos", Short: "Permite usar bloques de comandos en el mundo.", Long: "Activa los bloques de comandos, necesarios para mapas de aventuras y redstone avanzada. No afecta a jugadores normales.", Impact: nil},
	{Key: "spawn_protection", Title: "Protección del spawn", Short: "Radio alrededor del spawn donde solo los operadores pueden construir.", Long: "Radio en bloques alrededor del punto de aparición donde los no-operadores no pueden construir ni destruir. Reduce a 0 para mapas creativos.", Example: "16", Impact: nil},
}

var byKey map[string]Help

func init() {
	byKey = map[string]Help{}
	for _, h := range Entries {
		byKey[h.Key] = h
	}
}

// Get devuelve la ayuda de una opción.
func Get(key string) (Help, bool) {
	h, ok := byKey[key]
	return h, ok
}

// Keys devuelve las claves del catálogo ordenadas.
func Keys() []string {
	out := make([]string, 0, len(byKey))
	for k := range byKey {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Has indica si el campo tiene entrada en el catálogo.
func Has(key string) bool {
	_, ok := byKey[key]
	return ok
}

// FieldsList devuelve la lista de claves disponible (para la pista de --explain).
func FieldsList() string {
	keys := Keys()
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += ", "
		}
		out += k
	}
	return out
}
