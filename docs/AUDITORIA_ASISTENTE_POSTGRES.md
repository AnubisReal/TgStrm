# Auditoría del asistente de PostgreSQL

Fecha: 2026-09-28
Alcance: configuración inicial de PostgreSQL en el binario nativo y en Docker/Portainer.

## Hallazgos y decisiones

| Estado | Hallazgo | Resolución y motivo |
| --- | --- | --- |
| Corregido | El asistente probaba la conexión, pero la base objetivo tenía que existir previamente. | Se añadió la opción explícita «Crear la base si falta». La creación exige `CREATEDB` o superusuario, asigna la propiedad al rol que la creó y no elimina ni reemplaza bases existentes. |
| Corregido | La prueba y el guardado usaban el mismo paso, lo que habría hecho la prueba destructiva si se creaba la base. | `Probar conexión` ahora solo valida conexión/permisos; la prueba de integración verifica que la base siga ausente. `Guardar` crea la base que falte y ejecuta las migraciones. |
| Corregido | El guardado de la base llamaba a la preparación de administrador antes de que el helper tuviera acceso a la conexión nueva. | `ensureDefaultAdmin` recibe la conexión explícitamente; la integración confirma que la cuenta inicial queda preparada y que el servicio reinicia con los datos. |
| Corregido | La página no ayudaba a distinguir PostgreSQL local de PostgreSQL en Docker y el selector TLS usaba un control visual distinto. | El estado comunica el modo de ejecución. En modo nativo se ofrece un sondeo TCP local y enlaces oficiales del sistema; en el Stack integrado el formulario sugiere el host/usuario `postgres`/`tgstrm`; en modo externo deja la dirección bajo control del usuario. TLS usa el control visual del sistema de ajustes existente. |
| Añadido | El Stack anterior no incluía PostgreSQL. | Se conservaron intactas las plantillas que esperan PostgreSQL externo y se añadieron dos plantillas integradas para instalación nueva. El servicio no publica el puerto de PostgreSQL; el volumen de Postgres es independiente de `/data` y de la biblioteca. |
| Corregido | Usar el usuario administrador de la imagen oficial para TgStrm guardaría una credencial superusuario en la aplicación. | `postgres-init.sh` crea un rol de aplicación `tgstrm`, dueño solo de la base de TgStrm. Portainer configura por separado la contraseña de administración del clúster y la credencial limitada que se introduce en la web. |
| Añadido | PostgreSQL podía no estar listo cuando TgStrm arrancara. | Los nuevos Stacks usan `healthcheck` con `pg_isready` y `depends_on: service_healthy`, como recomienda Compose. |
| Añadido | Las pruebas del flujo PostgreSQL no corrían en CI. | CI usa un servicio PostgreSQL temporal para ejecutar el caso de creación, migración, reinicio y rechazo de una migración modificada. |

## Seguridad y compatibilidad revisadas

- El mensaje de usuario no expone errores SQL ni credenciales. El único error de privilegios mostrado es el requisito de `CREATEDB`.
- El nuevo campo `create_database` se interpreta como `false` cuando una versión antigua del frontend no lo manda. Se mantiene compatible el flujo previo de conectar a una base existente.
- El nombre del catálogo para `CREATE DATABASE` se cita con `pgx.Identifier`; se limita a 63 bytes para evitar el truncado silencioso que PostgreSQL aplica a identificadores demasiado largos.
- Si otro proceso crea la base a la vez, se vuelve a conectar ante el error PostgreSQL `42P04`; no se intenta limpiar ni recrear nada.
- Ninguna migración SQL nueva fue necesaria. El formato de `database.enc`, las claves de `/data` y el contenido de las tablas existentes no cambian.
- En Docker la base solo está en una red interna compartida con TgStrm. TgStrm conserva una red de salida para Telegram/TMDB y, según la plantilla, otra red hacia NPM. Las plantillas antiguas continúan usando la base externa ya configurada.
- Los Stacks nuevos crean el rol administrador `postgres` y un rol distinto `tgstrm`. Este último es propietario de la base de la aplicación, pero no es superusuario ni tiene `CREATEDB`; puede ejecutar el esquema y las migraciones necesarias. `pg_trgm`, que usa una migración existente, es una extensión marcada como confiable y admite instalación por el propietario con `CREATE` en la base.
- PostgreSQL 18 usa el punto de montaje oficial `/var/lib/postgresql`; se mantiene una etiqueta mayor fija (`postgres:18`). Actualizar entre versiones mayores requiere una migración/copia independiente.
- La comprobación local solo confirma que hay un proceso escuchando en `127.0.0.1:5432`; el botón «Probar conexión» valida después que sea PostgreSQL y que las credenciales/TLS funcionen. TgStrm no descarga, instala ni inicia servicios del sistema con privilegios.
- La imagen oficial solo aplica `POSTGRES_PASSWORD`, `POSTGRES_USER` y `POSTGRES_DB` en un volumen vacío. Cambiar variables en Portainer no rota credenciales de un clúster ya inicializado; hay que actualizar primero el rol y luego la conexión cifrada en TgStrm.

## Verificación ejecutada

- `go test ./...`: correcto.
- `go vet ./...`: correcto.
- `python3 scripts/test-postgres.py`: correcto con clúster temporal aislado y `-race`; también ejecuta el script de inicialización del Stack, comprueba que el usuario TgStrm sea propietario de su base y que no tenga `SUPERUSER` ni `CREATEDB`; valida que probar no crea la base, guardar sí la crea, arranque de migraciones, creación del administrador, reinicio persistente y detección de checksum alterado.
- `npm run build` en `web`: correcto (TypeScript y Vite).
- Compilación cruzada del binario sin CGO: correcta para Linux amd64/arm64, macOS Intel/Apple Silicon y Windows amd64.
- Análisis YAML de las cuatro plantillas Compose: correcto. No se ejecutó Docker Compose en este equipo.
- `git diff --check`: ejecutado después de los cambios, sin errores de formato.

## Fuentes oficiales consultadas

- PostgreSQL: `CREATE DATABASE` y privilegio `CREATEDB`: <https://www.postgresql.org/docs/current/sql-createdatabase.html>
- Descargas PostgreSQL y paquetes por sistema: <https://www.postgresql.org/download/>
- PostgreSQL para macOS: <https://www.postgresql.org/download/macos/> (informa que el instalador EDB se aloja fuera de los servidores de la comunidad y también enumera Postgres.app y Homebrew).
- PostgreSQL para Windows: <https://www.postgresql.org/download/windows/>.
- PostgreSQL para Linux: <https://www.postgresql.org/download/linux/>.
- PostgreSQL `pg_trgm` y la categoría de extensión confiable: <https://www.postgresql.org/docs/current/pgtrgm.html>.
- Imagen oficial PostgreSQL: variables iniciales, persistencia y cambio de ruta de volumen en la versión 18: <https://github.com/docker-library/docs/blob/master/postgres/README.md>.
- Docker Compose: orden de arranque y `service_healthy`: <https://docs.docker.com/compose/how-tos/startup-order/>.
