---
name: terminal-capture
description: Captura la pantalla de una terminal/TUI en ejecución (Windows) a PNG para revisión visual con un modelo de visión. Usa mss + pyautogui + Win32 para localizar la ventana por título, activarla, opcionalmente enviar teclas y recortar el área de cliente. Úsala cuando haya que generar capturas reales de una app de terminal (por ejemplo la TUI de badomens) en varios tamaños y pasarlas a un subagente de visión.
---

# Skill: captura de terminal (PTY → PNG)

## Propósito

Producir capturas **reales** de una aplicación de terminal/TUI en ejecución y
entregarlas a un modelo de visión. Complementa (no sustituye) a los snapshots de
texto deterministas: la captura valida composición, recortes, jerarquía y que el
footer esté fijo.

## Cuándo usarla

- Tras un cambio de UI en una TUI y antes de darla por buena.
- Para revisar en 40×12, 70×20, 110×30 y 132×40 (o los tamaños del contrato).
- Cuando el contrato exige "capturas PNG + revisión visual".

## Requisitos (una vez)

```powershell
python -m pip install --user mss pillow pyautogui
```

Windows con sesión interactiva de escritorio (no sirve un servicio headless).
Si el SO está escalado (DPI > 100 %), el script lo compensa con
`SetProcessDPIAware`.

## Uso

```powershell
# Localizar ventanas visibles (para saber el --title correcto)
python scripts/term_shot.py --list

# Capturar el área de cliente de una ventana por título
python scripts/term_shot.py --title "badomens" --out docs/capturas/home_110x30.png

# Activar la ventana, enviar teclas y capturar
python scripts/term_shot.py --title "badomens" --keys "enter,down,down" `
    --out docs/capturas/detalle_110x30.png

# Vista completa (toda la pantalla) y aumentar x2 para legibilidad
python scripts/term_shot.py --full --scale 2 --out pantalla.png
```

Opciones principales:

| Flag | Uso |
|---|---|
| `--title SUBSTR` | Subcadena del título de la ventana a capturar |
| `--out RUTA` | PNG de salida (obligatorio salvo `--list`) |
| `--list` | Lista ventanas visibles y termina |
| `--full` | Captura la pantalla completa (monitor primario) |
| `--client` / `--frame` | Área de cliente (def.) o marco completo de la ventana |
| `--keys "a,b,type:hola,sleep:0.5"` | Teclas a enviar antes de capturar |
| `--wait S` | Espera antes de capturar (por defecto 0.4 s) |
| `--scale N` | Reescala la imagen con vecino más cercano (legibilidad) |

## Flujo recomendado para una TUI

1. Fija el tamaño del terminal con las teclas de la TUI o redimensiona la
   ventana a un tamaño conocido (p. ej. 110×30 celdas).
2. Lanza la TUI (`go run ./cmd/badomens tui` o el binario) en esa ventana.
3. Navega con `--keys` hasta el estado a capturar; evita depender del ratón.
4. Captura cada pantalla y cada tamaño en `docs/capturas/` (o un directorio
   temporal) con un nombre sin ambigüedad: `<pantalla>_<w>x<h>.png`.
5. **Pasa los PNG a un subagente de visión** (`visual-qa`) con la rúbrica:
   recortes/solapes, jerarquía de título/pestaña/estado/atajos, dónde se escribe
   y qué tiene el foco, consola fuera de su pestaña, footer fijo, densidad y
   espacio, y claridad de errores/estados desconocidos. Pide hallazgos por
   coordenadas/área, no una opinión genérica.
6. Registra en el PR: modelo usado, rutas de los PNG y hallazgos accionables.

## Reglas no negociables

- Una captura **no** pasa por haber trabajado la UI; pasa cuando el subagente de
  visión devuelve PASS y los tests de teclado/estado también pasan.
- No captures con la ventana tapada ni minimizada: el script activa la ventana,
  pero verifica que el `--title` no coincida con varias (usa `--list`).
- No automatices el ratón salvo que la pantalla solo responda a clics; prioriza
  teclas para que la captura sea reproducible.
- Distingue "captura revisada" de "captura generada": no afirmes revisión visual
  sin una imagen examinada.

## Scripts

- `scripts/term_shot.py` — localiza la ventana (Win32), la activa, opcionalmente
  envía teclas y guarda un PNG del área elegida con `mss`.
