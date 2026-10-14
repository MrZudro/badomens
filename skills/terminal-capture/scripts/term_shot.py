#!/usr/bin/env python3
"""Captura una ventana de terminal/TUI a PNG (Windows).

Localiza una ventana visible por una subcadena del título, la activa, envía
teclas opcionales y captura el área de cliente (o el marco completo, o toda la
pantalla) con mss. Pensado para alimentar a un modelo de visión con capturas
reales de una TUI.

Ejemplos:
    python term_shot.py --list
    python term_shot.py --title badomens --out home.png
    python term_shot.py --title badomens --keys "enter,down,down" --out detalle.png
    python term_shot.py --full --scale 2 --out pantalla.png

Requisitos: mss, pillow, pyautogui (ver SKILL.md).
"""
from __future__ import annotations

import argparse
import ctypes
import sys
import time
from ctypes import wintypes

try:
    import mss
    from mss.tools import to_png
except ImportError:  # pragma: no cover
    sys.exit("Falta 'mss'. Instala: python -m pip install --user mss pillow pyautogui")

user32 = ctypes.windll.user32
try:
    user32.SetProcessDPIAware()  # evita recortes con DPI > 100 %
except Exception:
    pass


class RECT(ctypes.Structure):
    _fields_ = [
        ("left", wintypes.LONG),
        ("top", wintypes.LONG),
        ("right", wintypes.LONG),
        ("bottom", wintypes.LONG),
    ]


def list_windows() -> list[tuple[int, str]]:
    out: list[tuple[int, str]] = []
    proc = ctypes.WINFUNCTYPE(ctypes.c_bool, wintypes.HWND, wintypes.LPARAM)

    def cb(hwnd, _):
        if not user32.IsWindowVisible(hwnd):
            return True
        n = user32.GetWindowTextLengthW(hwnd)
        if n:
            buf = ctypes.create_unicode_buffer(n + 1)
            user32.GetWindowTextW(hwnd, buf, n + 1)
            title = buf.value.strip()
            if title:
                out.append((int(hwnd), title))
        return True

    user32.EnumWindows(proc(cb), 0)
    return out


def find_windows(substr: str) -> list[tuple[int, str]]:
    low = substr.lower()
    return [(h, t) for h, t in list_windows() if low in t.lower()]


def window_rect(hwnd: int, client: bool) -> tuple[int, int, int, int]:
    r = RECT()
    if client:
        user32.GetClientRect(hwnd, ctypes.byref(r))
        pt = wintypes.POINT(r.left, r.top)
        user32.ClientToScreen(hwnd, ctypes.byref(pt))
        left, top = pt.x, pt.y
        width, height = r.right - r.left, r.bottom - r.top
    else:
        user32.GetWindowRect(hwnd, ctypes.byref(r))
        left, top = r.left, r.top
        width, height = r.right - r.left, r.bottom - r.top
    return left, top, width, height


def send_keys(spec: str) -> None:
    import pyautogui

    pyautogui.PAUSE = 0.05
    for raw in spec.split(","):
        token = raw.strip()
        if not token:
            continue
        if token.startswith("type:"):
            pyautogui.write(token[5:])
        elif token.startswith("sleep:"):
            time.sleep(float(token[6:]))
        else:
            pyautogui.press(token)


def grab(x: int, y: int, w: int, h: int, out: str, scale: int) -> None:
    region = {"left": x, "top": y, "width": max(1, w), "height": max(1, h)}
    factory = getattr(mss, "MSS", None) or mss.mss  # mss>=8 renombra a MSS
    with factory() as sct:
        shot = sct.grab(region)
        to_png(shot.rgb, shot.size, output=out)
    if scale and scale > 1:
        try:
            from PIL import Image

            im = Image.open(out)
            im = im.resize((im.width * scale, im.height * scale), Image.NEAREST)
            im.save(out)
        except Exception as exc:  # pragma: no cover
            print(f"aviso: no se pudo reescalar ({exc})", file=sys.stderr)


def main() -> int:
    ap = argparse.ArgumentParser(description="Captura una ventana de terminal a PNG.")
    ap.add_argument("--title", help="subcadena del título de la ventana")
    ap.add_argument("--out", help="PNG de salida")
    ap.add_argument("--list", action="store_true", help="lista ventanas visibles y sale")
    ap.add_argument("--full", action="store_true", help="captura la pantalla completa")
    ap.add_argument("--frame", action="store_true", help="usa el marco completo, no el área de cliente")
    ap.add_argument("--keys", default="", help="teclas a enviar antes de capturar")
    ap.add_argument("--wait", type=float, default=0.4, help="espera antes de capturar (s)")
    ap.add_argument("--scale", type=int, default=1, help="reescala xN (vecino más cercano)")
    args = ap.parse_args()

    if args.list:
        for hwnd, title in list_windows():
            print(f"{hwnd:>10}  {title}")
        return 0

    if not args.out:
        ap.error("--out es obligatorio (o usa --list)")

    if args.full:
        x, y, w, h = 0, 0, user32.GetSystemMetrics(0), user32.GetSystemMetrics(1)
    else:
        if not args.title:
            ap.error("--title es obligatorio salvo con --full o --list")
        matches = find_windows(args.title)
        if not matches:
            sys.exit(f"no se encontró ninguna ventana con «{args.title}» (usa --list)")
        if len(matches) > 1:
            for hwnd, title in matches:
                print(f"coincide {hwnd}: {title}", file=sys.stderr)
            sys.exit("título ambiguo: afina --title")
        hwnd = matches[0][0]
        try:
            if user32.IsIconic(hwnd):
                user32.ShowWindow(hwnd, 9)  # SW_RESTORE
            user32.SetForegroundWindow(hwnd)
        except Exception:
            pass
        time.sleep(0.2)
        x, y, w, h = window_rect(hwnd, client=not args.frame)

    if args.keys:
        send_keys(args.keys)
    time.sleep(max(0.0, args.wait))
    grab(x, y, w, h, args.out, args.scale)
    print(args.out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
