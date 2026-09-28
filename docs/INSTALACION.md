# Instalación, actualización y recuperación

## Antes de empezar

TgStrm necesita una base PostgreSQL accesible, una carpeta privada persistente para su estado y dos destinos con escritura para películas y series. El estado contiene credenciales cifradas, sesión Telegram, caché y copias; los STRM viven en bibliotecas externas. Ambos sobreviven a una actualización, pero se respaldan de forma distinta.

## Ejecución nativa

Descargar la release correspondiente, extraerla y ejecutar:

```sh
chmod +x tgstrm
mkdir -p /ruta/privada/tgstrm-data
TGSTRM_DATA_DIR=/ruta/privada/tgstrm-data ./tgstrm
```

Antes de extraer un archivo descargado, verificarlo con el `SHA256SUMS`
publicado en la misma release. En macOS/Linux: `shasum -a 256 -c SHA256SUMS`;
en Windows: `Get-FileHash .\tgstrm_*.zip -Algorithm SHA256`.

En Windows PowerShell:

```powershell
New-Item -ItemType Directory -Force C:\TgStrm\data
$env:TGSTRM_DATA_DIR = 'C:\TgStrm\data'
.\tgstrm.exe
```

La dirección predeterminada es `127.0.0.1:5151`. Para poner un proxy inverso delante del binario:

```sh
TGSTRM_ADDR=0.0.0.0:5151
TGSTRM_ALLOW_REMOTE=1
TGSTRM_TRUSTED_PROXY_CIDRS=SUBRED_REAL_DEL_PROXY
```

No abrir el puerto a Internet directamente. El proxy debe terminar HTTPS y enviar `X-Forwarded-Proto: https`. La aplicación aceptará esa cabecera solo si la IP inmediata pertenece a `TGSTRM_TRUSTED_PROXY_CIDRS`.

Al abrir el panel por primera vez, introducir PostgreSQL en el asistente y conectar Telegram. La configuración y la sesión quedarán dentro de `TGSTRM_DATA_DIR` tras reiniciar o actualizar el binario.

En el asistente se puede comprobar si PostgreSQL escucha en el puerto local `5432`, crear la base indicada si aún no existe o conectarse a una existente. Para crearla, el usuario de PostgreSQL necesita el permiso `CREATEDB`; el botón de prueba solo valida conexión y permisos, y no crea ni modifica bases. La aplicación nunca elimina una base existente.

Si PostgreSQL no está instalado, el asistente enlaza las páginas oficiales de descarga para macOS, Windows y Linux. La página de PostgreSQL advierte que sus instaladores interactivos para macOS y Windows los hospeda EDB; en Linux recomienda paquetes de las distribuciones. TgStrm no descarga ni instala servicios del sistema automáticamente.

## Portainer y Docker

- `deploy/compose.portainer.yml` sirve para NPM que alcanza TgStrm mediante el host y liga `127.0.0.1:5151` por defecto.
- `deploy/compose.portainer.npm.yml` sirve para NPM en Docker: ambos comparten una red y NPM llega a `tgstrm:5151`.
- `deploy/compose.portainer.postgres.yml` es la instalación nueva recomendada con PostgreSQL integrado y un volumen independiente para la base.
- `deploy/compose.portainer.postgres.npm.yml` es la variante integrada para NPM dentro de Docker; NPM debe estar conectado a la red externa que se configure en Portainer.

Copiar los valores de `deploy/portainer.env.example` al Stack y cambiar al menos:

```text
TGSTRM_IMAGE=ghcr.io/PROPIETARIO_GITHUB/tgstrm:X.Y.Z
POSTGRES_PASSWORD=SECRETO_ADMIN_DE_POSTGRES
TGSTRM_DATABASE_PASSWORD=SECRETO_DE_LA_CUENTA_TGSTRM
TGSTRM_DATA_DIR_HOST=/instalaciones/tgstrm/data
TGSTRM_MOVIES_DIR_HOST=/extraible2/strm/movies
TGSTRM_SHOWS_DIR_HOST=/extraible2/strm/series
PUID=1000
PGID=1000
```

Crear esas tres rutas y permitir escritura a `PUID:PGID`. En el panel, los destinos deben ser `/media/movies` y `/media/shows`: son las rutas del contenedor, no las del host.

En los dos Stack con PostgreSQL integrado, establece dos secretos distintos en Portainer. `POSTGRES_PASSWORD` es solo para la cuenta administradora del servidor y no se introduce en TgStrm. El script de inicio del volumen crea una cuenta `tgstrm` limitada a su base, usando `TGSTRM_DATABASE_PASSWORD`; en el asistente web elige la base `tgstrm`, el usuario `tgstrm` y esa segunda contraseña. La cuenta de la aplicación no es superusuario ni recibe `CREATEDB`. El servidor de base de datos no publica puertos al host y solo comparte una red interna con TgStrm; el volumen `postgres_data` es independiente de `/data` y de las carpetas de STRM. Usa la etiqueta mayor fija `postgres:18` y conserva ese volumen al actualizar. Un cambio de versión mayor de PostgreSQL requiere un procedimiento de actualización y copia de seguridad propio; no basta con cambiar el número de etiqueta.

La imagen oficial aplica `POSTGRES_USER`, `POSTGRES_DB` y `POSTGRES_PASSWORD` solo al inicializar un volumen vacío. Cambiar estas variables después no rota la contraseña en un volumen ya creado; haz esa rotación con PostgreSQL y actualiza después la conexión guardada en TgStrm.

Para NPM en Docker, consultar `docker network inspect NOMBRE_RED` y usar su subred real —por ejemplo `172.18.0.0/16`— en `TGSTRM_TRUSTED_PROXY_CIDRS`; nunca una red abierta. En NPM crear un Proxy Host HTTPS hacia `tgstrm`, puerto `5151`, con WebSocket activado.

La imagen separa el estado persistente del sistema de archivos efímero del
contenedor: `/data` debe seguir montado al mismo directorio del host al
actualizar. PostgreSQL conserva sus propios datos en el volumen independiente
`postgres_data` si se usa el Stack integrado; con el Stack externo, conserva los
datos en el servidor que hayas configurado. Los destinos `/media/*`
son otros montajes independientes. Si se elimina o cambia cualquiera de esos
montajes, el reemplazo de imagen no puede conservar los datos que estaban allí.

## Actualizar

Crear primero una copia en **Configuración → Copias** y confirmar que aparece
como válida/restaurable. Sustituir luego la imagen por un tag concreto `X.Y.Z`,
recrear el Stack conservando los tres montajes y las mismas variables de entorno,
esperar el estado saludable y revisar Actividad/Trabajos. El nuevo binario
comprueba y aplica solo las migraciones pendientes al arrancar. Antes de hacer
esto en una instalación importante, conservar también una copia del directorio
`TGSTRM_DATA_DIR` y del PostgreSQL externo.

Las migraciones publicadas se dejan intactas: TgStrm comprueba sus checksums,
registra cada migración en la misma transacción que la aplica y aborta si la DB
contiene una versión desconocida o posterior. Una nueva imagen de TgStrm no
actualiza la versión principal del servidor PostgreSQL. Actualizar PostgreSQL
de una versión principal a otra es una operación separada y requiere seguir el
procedimiento oficial de PostgreSQL con su propia copia verificada.

Nunca borrar `TGSTRM_DATA_DIR` al actualizar: contiene la clave que descifra la conexión PostgreSQL y la sesión Telegram. No se garantiza bajar el binario después de aplicar migraciones nuevas: el binario anterior detecta que la DB tiene un esquema posterior y se niega a arrancar. Para volver atrás, detener TgStrm y restaurar primero la copia previa compatible de PostgreSQL y `/data`, y luego usar el tag anterior. No apuntar el binario antiguo a una DB ya migrada.

## Recuperación

Una copia de TgStrm contiene el volcado PostgreSQL, configuración cifrada y manifiesto de hashes. No incluye caché multimedia ni STRM, porque se recrean o viven en discos externos.

La imagen Docker instala clientes PostgreSQL versionados para las versiones
principales 14–18 y usa el cliente de la misma versión principal que el servidor
para crear/verificar/restaurar dumps. En una instalación nativa, `pg_dump` y
`pg_restore` de esa misma versión principal deben estar disponibles en `PATH`.

1. Conservar o restaurar el mismo directorio persistente, incluidos `master.key` y `database.enc`.
2. Arrancar TgStrm con acceso a PostgreSQL.
3. Ir a **Configuración → Copias** y ejecutar la restauración validada.
4. Comprobar salud y reconciliar STRM si también se restauraron bibliotecas externas.

No copiar claves o sesiones en GitHub, correo, mensajes o incidencias. Para trasladar la instalación, mover su directorio persistente por un canal privado y protegido junto con su copia PostgreSQL.
