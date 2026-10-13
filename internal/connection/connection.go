// Package connection calcula, sin que el usuario tenga que deducirlo, cómo
// conectarse a una instancia (RF-2): dirección preferida, nombre MagicDNS,
// versión, loader necesario y los pasos para el jugador.
//
// La información se persiste en el registro de la instancia y se recalcula en
// cada arranque (la IP del tailnet puede cambiar).
package connection

import (
	"fmt"
	"strings"
	"time"
)

// Info es la información de conexión de una instancia.
type Info struct {
	// Instance es el slug de la instancia.
	Instance string `json:"instance,omitempty"`
	// Name es el nombre legible («Survival»).
	Name string `json:"name,omitempty"`
	// Host es la IP del tailnet (100.x.y.z).
	Host string `json:"host,omitempty"`
	// MagicDNS es el nombre del tailnet (maquina.tail1234.ts.net), si existe.
	MagicDNS string `json:"magic_dns,omitempty"`
	// Port es el puerto publicado.
	Port int `json:"port"`
	// Address es "IP:puerto" (la preferida para copiar).
	Address string `json:"address,omitempty"`
	// AddressDNS es "nombre:puerto".
	AddressDNS string `json:"address_dns,omitempty"`
	// Edition es la edición ("Java").
	Edition string `json:"edition"`
	// McVersion es la versión de Minecraft.
	McVersion string `json:"mc_version"`
	// Engine es el motor (paper, fabric…).
	Engine string `json:"engine"`
	// Loader es el loader concreto ("Fabric 0.16.x", "Forge 52.x").
	Loader string `json:"loader,omitempty"`
	// ModsNeeded indica que el jugador debe instalar loader/mods.
	ModsNeeded bool `json:"mods_needed"`
	// OnlineMode indica si se exige cuenta oficial.
	OnlineMode bool `json:"online_mode"`
	// Whitelisted indica si hay lista blanca.
	Whitelisted bool `json:"whitelisted"`
	// PlayerSteps son los pasos para el jugador en lenguaje llano.
	PlayerSteps []string `json:"player_steps,omitempty"`
	// ResolvedAt es cuándo se calculó.
	ResolvedAt time.Time `json:"resolved_at"`
	// Warnings son avisos (sin IP, MagicDNS desactivado…).
	Warnings []string `json:"warnings,omitempty"`
}

// Input son los datos necesarios para calcular la conexión.
type Input struct {
	Instance   string
	Name       string
	Engine     string
	Version    string
	Port       int
	HostIP     string // IP del tailnet; vacío si no disponible.
	MagicDNS   string // nombre del tailnet; vacío si no hay MagicDNS.
	Loader     string
	ModsNeeded bool
	OnlineMode bool
	Whitelist  bool
	Now        time.Time
}

// Derive calcula la información de conexión a partir de los datos de entrada.
func Derive(in Input) Info {
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	info := Info{
		Instance:    in.Instance,
		Name:        in.Name,
		Host:        in.HostIP,
		MagicDNS:    in.MagicDNS,
		Port:        in.Port,
		Edition:     "Java",
		McVersion:   in.Version,
		Engine:      in.Engine,
		Loader:      in.Loader,
		ModsNeeded:  in.ModsNeeded,
		OnlineMode:  in.OnlineMode,
		Whitelisted: in.Whitelist,
		ResolvedAt:  now,
	}
	if in.HostIP != "" {
		info.Address = hostPort(in.HostIP, in.Port)
	}
	if in.MagicDNS != "" {
		info.AddressDNS = hostPort(in.MagicDNS, in.Port)
	}
	if in.HostIP == "" {
		info.Warnings = append(info.Warnings, "IP de Tailscale no disponible: ejecuta 'tailscale up' en el equipo del servidor")
	}
	if in.MagicDNS == "" {
		info.Warnings = append(info.Warnings, "MagicDNS desactivado: falta el nombre de conexión; usa la dirección IP")
	}
	info.PlayerSteps = steps(info)
	return info
}

// FromData calcula la conexión de una instancia a partir de sus datos.
func FromData(instance, name, engine, version string, port int, pins map[string]string,
	onlineMode, whitelistEnabled bool, hostIP, magicDNS string, now time.Time) Info {
	return Derive(Input{
		Instance:   instance,
		Name:       name,
		Engine:     engine,
		Version:    version,
		Port:       port,
		HostIP:     hostIP,
		MagicDNS:   magicDNS,
		Loader:     LoaderLabel(engine, pins),
		ModsNeeded: needsMods(engine),
		OnlineMode: onlineMode,
		Whitelist:  whitelistEnabled,
		Now:        now,
	})
}

// Preferred devuelve la dirección preferida para copiar (IP si existe; si no,
// el nombre MagicDNS).
func (i Info) Preferred() string {
	if i.Address != "" {
		return i.Address
	}
	return i.AddressDNS
}

// Invitation construye el mensaje listo para pegar en un chat.
func (i Info) Invitation() string {
	var b strings.Builder
	title := "🎮 Servidor"
	if i.Name != "" {
		title += " «" + i.Name + "»"
	} else if i.Instance != "" {
		title += " «" + i.Instance + "»"
	}
	title += " — Minecraft " + i.Edition + " " + i.McVersion
	b.WriteString(title + "\n")
	for n, step := range i.PlayerSteps {
		fmt.Fprintf(&b, "%d) %s\n", n+1, step)
	}
	return strings.TrimRight(b.String(), "\n")
}

// steps redacta los pasos para el jugador en lenguaje llano.
func steps(i Info) []string {
	var out []string
	out = append(out, "Instala Tailscale e inicia sesión con la cuenta que te compartí (o acepta la invitación a mi tailnet).")
	addr := i.Preferred()
	if addr == "" {
		addr = "la dirección que te pase"
	}
	out = append(out, "En Minecraft: Multijugador → Añadir servidor → Dirección: "+addr)
	if i.ModsNeeded {
		loader := i.Loader
		if loader == "" {
			loader = "el loader del servidor"
		}
		out = append(out, "Instala "+loader+" y los mismos mods que el servidor.")
	}
	if i.OnlineMode {
		out = append(out, "Entra con tu cuenta oficial de Minecraft.")
	} else {
		out = append(out, "Puedes entrar con cualquier nombre (el servidor no exige cuenta premium).")
	}
	if i.Whitelisted {
		out = append(out, "Pásame tu nombre de usuario para añadirte a la lista blanca.")
	}
	return out
}

// LoaderLabel describe el loader/pines concretos según el motor.
func LoaderLabel(engine string, pins map[string]string) string {
	switch engine {
	case "fabric":
		if v := pins["FABRIC_LOADER_VERSION"]; v != "" {
			return "Fabric " + v
		}
		return "Fabric"
	case "forge":
		if v := pins["FORGE_VERSION"]; v != "" {
			return "Forge " + v
		}
		return "Forge"
	case "neoforge":
		if v := pins["NEOFORGE_VERSION"]; v != "" {
			return "NeoForge " + v
		}
		return "NeoForge"
	}
	return ""
}

func needsMods(engine string) bool {
	return engine == "fabric" || engine == "forge" || engine == "neoforge"
}

func hostPort(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}
