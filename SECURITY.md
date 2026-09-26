# Seguridad

TgStrm gestiona una sesión de Telegram, una conexión PostgreSQL y credenciales
de TMDB. Esos datos se almacenan en el directorio persistente de cada
instalación y no deben aparecer en issues, pull requests, logs públicos ni
adjuntos de GitHub.

## Comunicar una vulnerabilidad

Usa la función de *private vulnerability reporting* del repositorio cuando el
propietario la habilite. Si no está disponible, contacta al propietario por un
canal privado. Incluye una reproducción mínima sin tokens, sesiones, URLs STRM
reales, nombres de grupos ni copias de `data/`.

No publiques una vulnerabilidad explotable hasta que exista una corrección y
una versión de release para actualizar.

## Operación segura

- Conserva juntos `data/master.key` y `data/database.enc`; perder uno impide
  recuperar la conexión PostgreSQL cifrada.
- No expongas el puerto de TgStrm directamente a Internet. Usa HTTPS y el
  acceso administrativo propio detrás de un proxy inverso.
- Declara únicamente el CIDR real del proxy en
  `TGSTRM_TRUSTED_PROXY_CIDRS`; nunca una red abierta.
- Antes de cada commit, usa `git add --dry-run .` y confirma que `data/`,
  `old/` y archivos de credenciales siguen excluidos.
