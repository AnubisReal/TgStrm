<p align="center">
  <img src="docs/assets/tgstrm-wordmark.svg" width="240" alt="TgStrm">
</p>

<p align="center">
  <strong>Tu catálogo de Telegram, convertido en una biblioteca multimedia estable.</strong><br>
  Sin sesiones en los enlaces. Sin depender de Vite para servir el panel.
</p>

<p align="center">
  <a href="https://github.com/AnubisReal/TgStrm/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/AnubisReal/TgStrm/actions/workflows/ci.yml/badge.svg"></a>
  <a href="https://github.com/AnubisReal/TgStrm/releases"><img alt="Release" src="https://img.shields.io/github/v/release/AnubisReal/TgStrm?display_name=tag&sort=semver"></a>
  <img alt="Go" src="https://img.shields.io/badge/Go-1.26-334d39?logo=go&logoColor=white">
  <img alt="Docker" src="https://img.shields.io/badge/Docker-ready-334d39?logo=docker&logoColor=white">
</p>

---

## Qué hace

| Catálogo | Biblioteca | Control |
| --- | --- | --- |
| Sincroniza grupos y topics de Telegram en tiempo real. | Genera `.strm` con UUID permanentes para películas y series. | Panel React integrado, API, WebSocket, salud, actividad y copias. |
| Usa TMDB para asociar títulos, pósteres y episodios. | Mantiene los STRM al añadir, editar o eliminar contenido del catálogo principal. | Espejo entre topics externos y el grupo principal. |

La aplicación conserva la configuración cifrada, la sesión de Telegram y el catálogo en almacenamiento persistente. Los enlaces `.strm` nunca contienen credenciales ni datos de sesión.

## Despliegue en Portainer

El Stack listo para importar es [deploy/stack.portainer.yml](deploy/stack.portainer.yml). Está preparado para Nginx Proxy Manager en Docker: TgStrm no publica puertos y NPM lo alcanza mediante la red privada compartida.

En **Portainer → Stacks → Add stack**, selecciona **Repository**, apunta a este repositorio y usa:

```text
Compose path: deploy/stack.portainer.yml
```

Mientras el repositorio continúe privado, configura también el registro `ghcr.io` en Portainer con una credencial de GitHub que tenga permiso de lectura de paquetes. Esa credencial se queda en Portainer; no se añade al Stack ni al repositorio.

Después añade estas variables con las rutas reales del servidor:

```text
TGSTRM_IMAGE=ghcr.io/anubisreal/tgstrm:v0.1.4
TGSTRM_DATA_DIR_HOST=/instalaciones/tgstrm/data
TGSTRM_MOVIES_DIR_HOST=/extraible2/strm/movies
TGSTRM_SHOWS_DIR_HOST=/extraible2/strm/series
TGSTRM_NPM_NETWORK=npm_default
TGSTRM_TRUSTED_PROXY_CIDRS=172.18.0.0/16
PUID=1000
PGID=1000
```

`TGSTRM_NPM_NETWORK` y `TGSTRM_TRUSTED_PROXY_CIDRS` deben corresponder a la red real de NPM. Se obtienen con `docker network inspect NOMBRE_RED`. En NPM crea un Proxy Host HTTPS hacia `tgstrm`, puerto `5151`, con WebSocket activado. En los ajustes de TgStrm usa `/media/movies` y `/media/shows`: son rutas internas del contenedor.

> Los tres directorios del host deben existir y pertenecer a `PUID:PGID`. `/data` es el estado de TgStrm; películas y series son bibliotecas independientes. No elimines `/data` al actualizar.

La plantilla de variables está en [deploy/portainer.env.example](deploy/portainer.env.example). Para una instalación nueva con PostgreSQL persistente usa [Portainer con NPM en el host](deploy/compose.portainer.postgres.yml) o [Portainer con NPM en Docker](deploy/compose.portainer.postgres.npm.yml). Las plantillas sin base integrada siguen disponibles para instalaciones que ya usan PostgreSQL externo.

## Releases nativas

Cada release `vX.Y.Z` incluye un único ejecutable para Linux amd64/arm64, macOS amd64/arm64 y Windows amd64. El binario contiene el panel web compilado: no requiere Node, Vite ni otro servidor web.

```sh
mkdir -p /ruta/privada/tgstrm-data
TGSTRM_DATA_DIR=/ruta/privada/tgstrm-data ./tgstrm
```

La versión actual está en [Releases](https://github.com/AnubisReal/TgStrm/releases). Verifica siempre la descarga con el archivo `SHA256SUMS` incluido.

## Desarrollo local

Requiere Go 1.26 y Node 22 o superior.

```sh
# Terminal 1
go run ./cmd/tgstrm

# Terminal 2
cd web && npm ci && npm run dev
```

La API escucha en `127.0.0.1:5151` y Vite en `5173`. Para exponerla en una red local se requiere `TGSTRM_ALLOW_REMOTE=1` y una dirección de escucha no loopback.

## Datos y seguridad

PostgreSQL es externo: TgStrm cifra y conserva su conexión, pero no instala ni administra PostgreSQL. El repositorio ignora `data/`, sesiones, claves, copias, STRM generados, el proyecto antiguo, dependencias y auditorías locales.

Antes de publicar cambios, comprueba el índice:

```sh
git status --ignored
git check-ignore -v data/database.enc old/session_name.session
git add --dry-run .
```

Nunca fuerces la inclusión de `data/` ni compartas una copia de seguridad, una sesión de Telegram o una clave.

## Operación

- [Instalación, actualización y recuperación](docs/INSTALACION.md)
- [Checklist de entrega y distribución](docs/ENTREGA_Y_DISTRIBUCION.md)
- [Stack de Portainer](deploy/stack.portainer.yml)
- [Última release](https://github.com/AnubisReal/TgStrm/releases/latest)

```sh
go vet ./...
go test ./...
(cd web && npm run build)
sh scripts/prepare-web-assets.sh
CGO_ENABLED=0 go build -trimpath -o bin/tgstrm ./cmd/tgstrm
```

`tgstrm healthcheck` es el comprobador usado también por Docker.
