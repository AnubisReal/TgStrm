# Entrega y distribución

Esta lista es el estado canónico del cierre de producto. Cada punto se marca
después de estar implementado y verificado; las pruebas reales de cada entorno
quedan registradas en la sección final.

## Base segura para GitHub

- [x] Inventario de estado local: `data/` contiene configuración cifrada,
  sesión de Telegram, caché, backups y bibliotecas de prueba.
- [x] `data/`, `old/`, auditorías locales, dependencias y certificados quedan
  fuera de Git y del contexto de construcción Docker.
- [x] El binario de producción no contiene secretos: las credenciales se crean
  o recuperan exclusivamente desde el volumen persistente.
- [x] Repositorio Git local inicializado y comprobación del primer índice antes
  de añadir un remoto de GitHub: ningún archivo de estado entra en él.
- [x] Repositorio privado creado en GitHub y publicación en GitHub Container
  Registry verificada. La visibilidad se mantiene privada hasta una decisión
  explícita de publicación.
- [ ] Elegir la licencia antes de hacer público el repositorio; no se ha
  inventado una licencia para el código ni para sus dependencias.

## Ejecutable de producción

- [x] La SPA de React se compila con Vite y queda embebida en el binario Go.
- [x] Las rutas del navegador, API, WebSocket y streaming comparten un origen;
  no hay Vite ni Node en producción.
- [x] La escucha remota requiere `TGSTRM_ALLOW_REMOTE=1`.
- [x] Las cabeceras `X-Forwarded-Proto` solo se aceptan desde CIDR de proxy
  declarados mediante `TGSTRM_TRUSTED_PROXY_CIDRS`.
- [x] El binario incorpora un `healthcheck` sin shell para contenedores.
- [x] Verificar el binario empaquetado en macOS local sin Vite: rutas SPA,
  cabeceras y `healthcheck` respondieron correctamente.

## Docker y Portainer

- [x] Dockerfile multi-etapa: Node solo compila la web, Go solo compila el
  binario y la imagen final no incluye compiladores ni dependencias de build.
- [x] Plantilla de Stack Portainer con volumen de estado separado y destinos
  externos independientes para películas y series.
- [x] Healthcheck, reinicio automático y `no-new-privileges` en el Stack.
- [ ] Construir y arrancar la imagen localmente.
- [ ] Probar la plantilla de Portainer con rutas reales y permisos del host.
- [ ] Probar Nginx Proxy Manager, WebSocket, HTTPS y cookie `Secure` desde la
  red Docker real.

## Releases nativos y automatización

- [x] CI de GitHub: tests Go, build web, binario embebido e imagen Docker.
- [x] Workflow de tag `v*`: binarios Linux amd64/arm64, macOS amd64/arm64 y
  Windows amd64; archivos adjuntos a GitHub Release.
- [x] Cada release genera `SHA256SUMS` para verificar los archivos descargados.
- [x] Workflow de tag `v*`: imagen multi-arquitectura Linux amd64/arm64 en
  GitHub Container Registry.
- [x] La imagen declara `org.opencontainers.image.source` apuntando al
  repositorio, para asociar el paquete GHCR con su código fuente.
- [x] Vincular `ghcr.io/anubisreal/tgstrm` al repositorio actual y conceder
  acceso de escritura al workflow en **Package settings → Manage Actions
  access**. El token del workflow también tiene `packages: write`.
- [x] Separar la publicación Docker por arquitectura: amd64 y arm64 se compilan
  en runners nativos y después se publican los tags multi-arquitectura. Así se
  evita compilar PostgreSQL y el resto de paquetes ARM bajo emulación QEMU.
- [x] Tag `v0.1.1` publicado: cinco binarios, `SHA256SUMS` e imagen
  multi-arquitectura generados correctamente en GitHub.
- [x] Release `v0.1.3` verificada tras repetir el flujo: cinco ejecutables,
  `SHA256SUMS` e imagen multi-arquitectura publicados. La repetición usa el tag
  existente y no lo mueve.
- [ ] Descargar y ejecutar el artefacto adecuado en cada plataforma de uso.

## Documentación operativa

- [x] Guía de instalación nativa, actualización y servicio del sistema.
- [x] Guía de Stack Portainer, redes NPM, permisos y rutas internas/externas.
- [x] Guía de copia y restauración que distingue estado TgStrm y biblioteca
  STRM.

## Registro de validación real

| Fecha | Entorno | Resultado | Observaciones |
| --- | --- | --- | --- |
| 2026-09-21 | macOS | Correcto | Binario embebido sin Vite, rutas SPA y `healthcheck` verificados localmente |
| 2026-09-21 | GitHub Actions | Correcto | CI y release `v0.1.1`: cinco binarios, SHA-256 e imagen Linux amd64/arm64 |
| 2026-09-26 | GitHub Actions | Parcial | CI correcto y cinco binarios `v0.1.3` compilados; el primer intento tuvo una ACL de GHCR y el reintento perdió comunicación con el runner durante el build emulado |
| 2026-09-27 | GitHub Actions | Correcto | Release `v0.1.3` repetida con imágenes arm64/amd64 en runners nativos; manifest multi-arquitectura, cinco binarios y `SHA256SUMS` publicados |
| Pendiente | Docker/Portainer | — | Docker no está disponible en el entorno actual; faltan volúmenes, permisos y actualización reales |
| Pendiente | NPM/HTTPS | — | WebSocket, cookies y streaming |
| Pendiente | Linux/Windows | — | Artefactos de release |
