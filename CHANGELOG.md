# Changelog

Todas las novedades relevantes de badomens se documentan aquí. El formato sigue
[Keep a Changelog](https://keepachangelog.com/es/1.1.0/) y el versionado es
semántico.

## [Unreleased]

### Añadido

- **F0 — Fundación**
  - Binario único `badomens` en Go con contrato JSON estable
    (`ok`, `exit_code`, `action`, `message`, `data`, `error`, `timestamp`).
  - Comandos `version`, `doctor`, `capacity`, `config`, `update`, `commands`.
  - Detección de capacidad del host (CPU/RAM) a través de Docker.
  - `doctor`: Docker, Docker Compose, Tailscale y coherencia de recursos.
  - `update --check`: comparación semver contra las releases de GitHub.
  - Servidor MCP por stdio (`mcp serve`) sobre un registro único de herramientas,
    con modo `--read-only`.
  - CI (Linux/macOS/Windows), GoReleaser y releases con binarios y checksums.
