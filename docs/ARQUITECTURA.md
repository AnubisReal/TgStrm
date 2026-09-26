# TgStrm: arquitectura y plan de producto

Estado: especificación inicial, 2026-09-10. No representa funcionalidades ya implementadas.
`old/` es referencia funcional; la nueva aplicación se desarrolla por módulos y se valida por etapas.

## 1. Objetivo y decisiones

Gestor de biblioteca multimedia de Telegram con catálogo persistente, streaming, generación STRM, organización TMDB y automatizaciones. Funcionará en red local y detrás de Nginx Proxy Manager (NPM) con dominio y HTTPS.

- Backend: Go, monolito modular, API HTTP versionada y WebSocket.
- Telegram: MTProto mediante gotd, sujeto a prueba de sincronización y persistencia antes de fijar versión.
- Datos: PostgreSQL; transacciones, claves estables, migraciones versionadas y restauración comprobada.
- Frontend: React, TypeScript estricto, Vite, React Router, TanStack Query, Tailwind, shadcn/ui y Lucide.
- Formularios: React Hook Form y Zod como propuesta; validación autoritativa también en Go.
- Contratos: OpenAPI para HTTP y esquemas versionados para eventos WebSocket; tipos frontend generados.
- Operación inicial: una instancia de aplicación y una PostgreSQL; sin Redis ni microservicios requeridos.
- Configuración funcional desde la web. `.env` opcional, sin credenciales funcionales obligatorias al arrancar.

## 2. Estructura prevista del repositorio

```text
cmd/tgstrm/                   Arranque del servidor
internal/
  app/                        Composición, inicio y cierre ordenado
  config/                     Configuración efectiva y procedencia
  auth/                       Usuarios, sesiones web y permisos
  telegram/                   Adaptador MTProto y sesiones Telegram
  sync/                       Actualizaciones, recuperación y reconciliación
  catalog/                    Chats, temas, mensajes y archivos
  library/                    Biblioteca, identificación y asociaciones TMDB
  streaming/                  HTTP Range y recuperación de archivos
  strm/                       Generación y actualización de enlaces
  automation/                 Reglas de reenvío y migraciones
  jobs/                       Trabajos persistentes, reintentos y cancelación
  realtime/                   WebSocket y entrega de eventos
  storage/postgres/           Consultas, transacciones y repositorios
  httpapi/                    Rutas, autorización y errores
  observability/              Salud, métricas, logs y auditoría
api/                          OpenAPI y contrato de eventos
migrations/                   Evolución versionada del esquema
web/src/
  app/                        Router, proveedores y estructura visual
  components/ui/              Componentes base compartidos
  features/                   auth, onboarding, chats, library, jobs, settings…
  lib/api/                    Cliente y tipos generados
  lib/realtime/               Conexión, reconexión e invalidaciones
deploy/                       Compose, arranque y guía NPM
tests/integration/            Persistencia, API, migraciones y sincronización
tests/e2e/                    Recorridos completos de la web
docs/                         Arquitectura, operación y decisiones
old/                          Referencia histórica
```

Se crearán los módulos al implementarlos, evitando carpetas vacías como sustituto de funcionalidad. La lógica de negocio no dependerá de componentes React ni de handlers HTTP. Telegram, TMDB y PostgreSQL se encapsularán en adaptadores con límites claros.

## 3. Red local, dominio y despliegue

La distribución servirá frontend, `/api/v1`, `/api/v1/events/ws` y streaming bajo el mismo origen. Vite se usará en desarrollo; en producción no será necesario ejecutar Node.

- LAN: acceso directo a IP y puerto configurables.
- Dominio: NPM termina TLS y reenvía a Go en la red interna.
- Activar soporte WebSocket en NPM; ajustar timeouts y probar conexiones largas y streaming con Range.
- El navegador usará rutas relativas y construirá `ws:` o `wss:` según su origen.
- La aplicación confiará en cabeceras forwarded exclusivamente desde proxies configurados. Lista explícita de hosts/orígenes autorizados; sin CORS abierto por defecto.
- Rutas profundas de React deben funcionar al pegar la URL o recargar. El fallback SPA nunca ocultará un 404 de API, streaming o assets.
- Configuración de URL pública, URL LAN y base preferida para STRM. No generar enlaces permanentes a partir de cualquier cabecera Host recibida.
- Si cambia el dominio, un trabajo podrá regenerar enlaces STRM con vista previa y progreso.
- LAN HTTP será un modo explícito. HTTPS usa cookies Secure; no degradar cookies al detectar una cabecera arbitraria. Se recomienda el dominio HTTPS también desde LAN cuando la red permita resolverlo.
- IP y dominio tienen cookies independientes: pueden exigir iniciar sesión en ambos orígenes, sin desconectar la cuenta Telegram.

Se documentarán puertos, volúmenes y permisos de carpetas multimedia. La aplicación podrá diagnosticar conectividad y mostrar la configuración NPM necesaria; no administrará certificados, DNS o Docker ajenos sin una integración específica.

## 4. Arranque sin .env obligatorio

Hay dos niveles distintos: infraestructura mínima (base de datos, puerto, directorio persistente) y configuración del producto (Telegram, TMDB, rutas, reglas).

La distribución para home server incluirá un arranque que genere secretos de infraestructura una sola vez y configure PostgreSQL automáticamente en red privada con volúmenes persistentes. Esos secretos no serán valores compartidos ni cambiarán al reiniciar. La modalidad con PostgreSQL externo permitirá introducir y probar su conexión en un asistente de bootstrap; el servidor podrá mostrar ese asistente antes de tener catálogo disponible. Un fallo posterior de PostgreSQL mostrará recuperación, nunca un falso primer arranque que cree otra instalación.

El primer arranque se protegerá con un código de instalación generado por instancia y mostrado en consola/log inicial local. Es una comprobación de posesión del servidor, sin editar `.env`; evita que un visitante del dominio se convierta en administrador antes que el propietario.

Asistente reanudable:

1. Reclamar instalación y crear administrador del panel.
2. Verificar almacenamiento y base de datos.
3. Introducir API ID y API hash de Telegram si faltan.
4. Vincular cuenta por teléfono, código y contraseña 2FA cuando corresponda; QR como mejora posterior validada con gotd.
5. Elegir grupos a sincronizar y directorios de salida.
6. Configurar opcionalmente TMDB, dirección pública y preferencias.
7. Mostrar progreso del primer escaneo y permitir acceder al catálogo parcial identificado como tal.

Con administrador existente se mostrará login; con sesión web válida se abrirá el panel. Si falta Telegram se pedirá vincularlo dentro del panel, sin reiniciar todo el asistente.

Precedencia: variables de proceso no vacías > `.env` no vacío > configuración persistida > valores predeterminados. La web mostrará valores impuestos por entorno como bloqueados y señalará su procedencia, sin revelar secretos. Distinguir cambios inmediatos de cambios que requieren reinicio. Un valor vacío no elimina secretos persistidos: eliminarlos requiere una acción explícita.

## 5. Autenticación y formularios

Separar autenticación administrativa de autorización Telegram. Entrar con teléfono a Telegram no sustituye la protección del panel.

- Contraseñas del panel con Argon2id; sesiones opacas persistidas con token almacenado mediante hash, caducidad y revocación.
- Cookie HttpOnly, SameSite y Secure bajo HTTPS; protección CSRF para acciones mutantes, límites de intentos y rotación al autenticar.
- Sesión web conservada tras reiniciar dentro de su vigencia; lista de dispositivos y opción de revocarlos.
- Sesión MTProto cifrada con clave persistente fuera de la base de datos, permisos restrictivos y copia protegida de la clave. Reiniciar/recrear contenedor no debe regenerarla.
- Códigos y contraseña 2FA Telegram no se guardan como configuración permanente ni aparecen en logs.
- Formularios con etiquetas visibles, semántica HTML, autocomplete correcto, pegado permitido, gestores de contraseñas, mostrar/ocultar contraseña, foco y navegación por teclado.
- Código OTP compatible con pegado completo; evitar varios inputs que rompan accesibilidad. Reenvío con espera real indicada por Telegram.
- Errores junto al campo y resumen cuando proceda; diferenciar validación, fallo de red y espera requerida, evitando revelar existencia de usuarios.
- Mantener valores no sensibles al fallar, botones con estado de envío y prevención de doble envío.
- Bootstrap, login y vinculación Telegram tendrán diseño y textos propios dentro del mismo sistema visual.

## 6. Catálogo y consistencia

Tablas conceptuales: users, web_sessions, telegram_accounts, chats, topics, messages, media_files, sync_states, scan_runs, jobs, job_attempts, library_items, tmdb_matches, strm_entries, forwarding_rules, settings, outbox_events y audit_events.

- Identidad Telegram con cuenta, tipo de peer y peer ID; evitar conversiones heurísticas por longitud o prefijos.
- Claves únicas por cuenta/chat/topic y cuenta/chat/message. El nombre del tema es mutable.
- Identificadores de 64 bits se transportarán como strings JSON para evitar pérdida de precisión en JavaScript.
- Tema General se modelará explícitamente; no confundir un chat sin foro con un topic remoto existente.
- Una fila por mensaje/archivo; no mantener un JSON completo del tema como segunda fuente de verdad.
- Relaciones y restricciones SQL, tamaño en bytes enteros, fechas UTC, borrados lógicos cuando hagan falta para resolver eventos tardíos.
- Asociaciones TMDB y ajustes del usuario separados de metadatos remotos.
- Índices para listar por chat/tema, fecha e ID; paginación por cursor con orden estable.
- Upserts repetibles, control del orden remoto y transacciones para datos relacionados.
- El escaneo histórico no sobrescribe actualizaciones más recientes. Mantener frontera y estado de escaneo, eventos concurrentes y reconciliación final.
- No interpretar respuesta incompleta o fallo de acceso como borrado. Marcar inaccesible/desactualizado y verificar.

## 7. MTProto y WebSocket

Dos subsistemas: Telegram→Go por MTProto; Go→web por WebSocket. El servidor sigue funcionando con cero navegadores conectados.

El sincronizador recuperará secuencias globales y por canal con el gestor de updates de gotd y las operaciones de diferencias de Telegram. No confundir el mayor message ID con el estado completo: también hay ediciones, borrados y cambios de temas. Cuando Telegram no permita recuperar todo el hueco se reindexará el ámbito afectado. Reconciliación periódica limitada y respeto a FloodWait.

Antes de confirmar avance de Telegram, los efectos o el evento recibido deben estar duraderamente registrados. Validar específicamente cómo coordinar el almacenamiento del gestor gotd con la transacción de catálogo; no asumir atomicidad entre escrituras independientes.

En una misma transacción PostgreSQL: actualizar catálogo y registrar evento outbox. Publicar después del commit. Si el proceso se cae entre commit y envío, el publicador recupera lo pendiente.

Contrato de evento: event_id, schema_version, type, resource_id, revision, occurred_at y payload mínimo. Tipos previstos: topic.updated, media.created/updated/deleted, sync.progress, job.progress y system.status.

- Una conexión autenticada por pestaña con suscripciones autorizadas y orígenes comprobados.
- Secuencias de entrega propias: no exponer ni reutilizar los cursores MTProto como cursores web.
- Entrega al menos una vez, deduplicación y revisiones; no prometer exactamente una vez.
- Heartbeat, reconexión con backoff y cursor de último evento.
- Retención limitada; si el cursor ya expiró, emitir resync_required y volver a consultar la API.
- Coordinar snapshot inicial y suscripción para no perder cambios entre ambos.
- Agrupar notificaciones, limitar colas y desconectar/reconciliar clientes lentos.
- TanStack Query invalida páginas afectadas; actualizaciones directas solo cuando se puede mantener orden, filtros y totales correctamente.
- Revocar sesiones también cierra WebSockets. No enviar tokens por query string ni registrar secretos.

## 8. Navegación y paneles

```text
/setup
/login
/dashboard
/telegram/accounts
/chats
/chats/:chatId
/chats/:chatId/topics/:topicId
/chats/:chatId/topics/:topicId/files/:messageId
/library/movies
/library/series
/library/:libraryId
/jobs
/jobs/:jobId
/automations
/automations/:ruleId
/activity
/system/health
/system/logs
/system/storage
/settings/general
/settings/telegram
/settings/tmdb
/settings/library
/settings/network
/settings/security
/settings/backups
```

chatId será una clave interna estable y no ambigua que resuelve cuenta y peer; la API expondrá también el identificador Telegram. El topicId de una ruta está siempre subordinado al chat. Selección, búsquedas y filtros navegables irán a parámetros de URL cuando tenga sentido.

Paneles:

- Resumen: catálogo, trabajos, última sincronización y errores que requieren atención.
- Grupos y temas: búsqueda, orden, filtros, selección múltiple, permisos, progreso y archivos.
- Trabajos: estado real, avance conocido o indeterminado, reintentos, errores y cancelación segura.
- Actividad: acciones del usuario y eventos relevantes, separado de logs técnicos.
- Salud: Telegram, base de datos, espacio, latencia, retraso de sincronización y WebSocket.
- Logs: filtros, paginación, seguimiento limitado y exportación saneada con ID de correlación.
- Configuración: formularios separados por área, prueba de conexión, guardado explícito y cambios pendientes.
- Backups: crear, listar, descargar con permiso, verificar y restaurar con reautenticación y modo mantenimiento.

Los controles operarán sobre una API autorizada; no exponer una consola shell o SQL como alternativa a funciones administrativas.

## 9. Dirección visual y calidad

Interfaz de gestión con identidad propia: navegación superior flotante tipo píldora y cápsulas laterales estrechas de iconos, encabezados con breadcrumbs, búsqueda clara, tablas cuidadas, acciones contextuales y paneles de detalle. Evitar llenar el inicio con todos los grupos o tarjetas decorativas sin utilidad.

Tokens de tipografía, espaciado, color, radios y movimiento; componentes compartidos con variantes. Temas claro/oscuro, diseño responsive para móvil, tablet y escritorio. Densidad adecuada para catálogos grandes. Virtualización cuando el volumen renderizado lo justifique, junto con paginación del servidor.

Diseñar estados completos: primera instalación, cargando, vacío, parcial, error, desconectado, reconectando y sin permisos. Skeletons con geometría estable, foco visible, contraste WCAG AA, movimiento reducido y objetivos táctiles adecuados. Los estados no dependerán solo del color.

Antes de construir todas las pantallas, fijar una dirección visual mediante login, listado de temas y detalle de tema. La aceptación visual incluirá capturas en móvil/escritorio y los estados de error; usar shadcn por sí solo no constituye un diseño terminado.

## 10. API, streaming y operaciones

HTTP `/api/v1` con autorización por recurso, errores estables, request_id, paginación y documentación. Trabajos largos devuelven 202 y job_id; no dependen de mantener una petición o pestaña abierta. Repetir solicitudes críticas usa claves de idempotencia. WebSocket informa; las acciones ordinarias siguen pasando por HTTP.

La autorización de Telegram tiene ciclo de vida independiente del catálogo. El cierre explícito usa `auth.logOut`, detiene el cliente MTProto y elimina la sesión cifrada, el perfil local y el reto de acceso, conservando credenciales de aplicación, catálogo, STRM, ajustes y trabajos durables. Una respuesta 401 de Telegram aplica el mismo aislamiento con estado `revoked`. Mientras no exista una conexión autorizada, las colas que requieren Telegram permanecen pendientes; tras teléfono, código y 2FA, el cliente de updates recupera diferencias y reanuda esas colas.

El streaming usa GET/HEAD y Range, sin transportar vídeo por WebSocket. Los reproductores externos necesitan credenciales independientes de la cookie administrativa: enlaces opacos revocables por biblioteca/recurso, con política de vigencia compatible con STRM. No introducir credenciales Telegram ni tokens administrativos en STRM. Renovar referencias de archivo Telegram cuando expiren. Probar seeks y reconexiones detrás de NPM.

TMDB, STRM y reenvíos se ejecutan en trabajos persistidos con reintentos acotados, progreso real y resultados por elemento. Las importaciones usan `catalog_imports` y `catalog_import_messages` como cola canónica: la selección completa se confirma antes de responder, un consumidor la reclama con `FOR UPDATE SKIP LOCKED` y Telegram la procesa secuencialmente. Generación STRM con escritura temporal y rename atómico, rutas saneadas y acceso limitado a raíces configuradas. Cancelar un trabajo no deshace efectos externos ya completados: el panel explicará qué terminó.

## 11. Evolución, backups y observabilidad

Migraciones numeradas y comprobadas; bloqueo de ejecución concurrente y verificación de versión. Evolución expandir→migrar datos→retirar estructuras cuando exista compatibilidad. No ejecutar DROP/recrear catálogo como actualización ordinaria. Migraciones destructivas con backup previo y plan de recuperación; no prometer rollback automático para toda migración.

Backups consistentes de PostgreSQL y secretos necesarios para restaurar sesiones, con cifrado, retención y copia fuera del mismo disco. Los backup jobs y su historial sobreviven a reinicios. Probar restauración en instalación limpia. Backup de datos sin clave de cifrado no es una recuperación completa.

Medir colas, retraso de sincronización, errores Telegram, latencia API, conexiones y espacio. Logs estructurados con rotación y redacción de secretos. Liveness y readiness separados; endpoints públicos mínimos y detalles solo autenticados.

## 12. Etapas y criterios de aceptación

1. **Fundación:** arranque sin .env, PostgreSQL, migraciones, login persistente, asistente, rutas y shell visual. Aceptación: reinicio y recarga de ruta profunda, LAN y HTTPS/NPM, cookies y bootstrap protegido.
2. **Catálogo fiable:** cuenta Telegram, chats/topics/media, escaneo reanudable, updates y WebSocket. Aceptación: crear/editar/borrar/renombrar, duplicados, orden alterado, corte de red, caída durante commit/envío y recuperación sin vaciar catálogo.
3. **Operación web:** trabajos, actividad, salud, logs, ajustes y backups. Aceptación: restauración real, permisos, sesiones revocadas y trabajos reanudados.
4. **Biblioteca:** streaming, STRM y TMDB. Aceptación: Range mediante proxy, enlaces por dominio/LAN, reinicios y cambios de base URL.
5. **Automatizaciones:** reenvíos y migración de temas. Aceptación: FloodWait, reintentos, prevención de bucles, cancelación y registro de resultados parciales.

Cada etapa incluye pruebas de integración pertinentes y recorridos web. No dar por resuelta la base por una demo que solo funciona conectada y sin reiniciar.

## 13. Referencias

- Telegram, updates y recuperación: https://core.telegram.org/api/updates
- gotd, recuperación: https://gotd.dev/docs/helpers/updates-recovery/
- NPM, plantilla con soporte WebSocket: https://github.com/NginxProxyManager/nginx-proxy-manager/blob/develop/backend/templates/proxy_host.conf
- OWASP, autenticación: https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html
- OWASP, sesiones: https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html
- OWASP, WebSocket: https://cheatsheetseries.owasp.org/cheatsheets/WebSocket_Security_Cheat_Sheet.html
- Formularios y autocomplete: https://web.dev/learn/forms/autofill
- shadcn/ui: https://ui.shadcn.com/docs
- PostgreSQL, backups: https://www.postgresql.org/docs/current/backup.html

## Espejo de topics

`/mirror` es el espacio global para observar vínculos y `/mirror/manage` permite administrarlos. Un vínculo une un topic externo con un topic existente del grupo principal. Telegram no ofrece topics anidados: internamente ambos siguen siendo hilos independientes y TgStrm conserva la relación.

Las reglas viven en `mirror_links`; cada archivo genera una entrega idempotente en `mirror_deliveries`. El update MTProto es la vía inmediata y una reconciliación desde PostgreSQL cubre reinicios o diferencias recuperadas. Cada entrega conserva el mismo `random_id`, estado y reintentos, por lo que cerrar la web o reiniciar el proceso no pierde el trabajo. El origen debe ser externo, el destino debe pertenecer al grupo principal y un destino solo admite un vínculo activo para impedir ciclos y mezclas ambiguas.

El comportamiento es unidireccional. Al crear el vínculo se elige entre empezar con archivos nuevos o incluir también los existentes. En el segundo caso el servidor pagina primero el historial completo del topic, registra una entrega idempotente por vídeo y continúa después con los updates en tiempo real; el estado y el progreso sobreviven a reinicios. Las eliminaciones remotas no se propagan. Las ediciones del pie sí se reflejan sobre el mismo mensaje de destino mediante su asociación `random_id`/`updateMessageID`; una cola persistente reintenta la operación y una reconciliación recupera cambios ocurridos durante una desconexión. El archivo recibido o editado en destino entra por el flujo normal de Telegram y reutiliza matching, reemplazo de episodios, STRM, actividad y WebSocket.

Referencias oficiales: https://core.telegram.org/api/forum, https://core.telegram.org/api/updates y https://core.telegram.org/method/messages.sendMedia.

## 14. Ajuste de navegación y home (acordado)

Navegación principal superior tipo píldora: Inicio, Catálogo, Biblioteca y Actividad; configuración y cuenta en acciones superiores. Se añaden cápsulas laterales compactas de iconos según la referencia visual, no un menú ancho con etiquetas; en móvil sus acciones se agrupan sin tapar contenido. El acceso público tiene cabecera de marca, sin enlaces privados inactivos. Fuente local Stara recuperada de old/fonts, con pesos reales medium, semibold y bold.

El home antiguo contenía búsqueda de canales/grupos, carga manual y gestor de caché. El nuevo home priorizará búsqueda, grupos recientes/favoritos, actividad reciente y estado resumido de sincronización. El catálogo completo vive en /chats; diagnóstico de sincronización sustituye al botón genérico de caché. Generación, TMDB, duplicados y reenvíos pertenecen al contexto del grupo/tema o a trabajos, no a un home saturado.

Primero se revisa la pantalla de acceso. No considerar autenticación completa hasta verificar persistencia, revocación y reinicios con PostgreSQL.

## 15. Dirección visual revisada — referencia del usuario

Esta sección sustituye las decisiones visuales anteriores que entren en conflicto.

- Conservar únicamente el logo diseñado y las fuentes Stara del acceso anterior.
- Paleta neutra: fondo gris claro, superficies blancas, textos grafito y selección casi negra. El logo conserva su identidad original. Eliminar verde decorativo, posters, ilustraciones, eslóganes y composición partida del login.
- La navegación conserva una estructura propia de píldoras superiores y cápsulas laterales; no incluye ni replica contenido, gráficos o tarjetas ajenas.
- Cabecera del panel: marca a la izquierda, navegación central dentro de píldora blanca con opción activa oscura, cápsulas independientes para herramientas y cuenta a la derecha.
- Lateral: pequeñas cápsulas verticales flotantes de iconos, agrupadas por función, con estado activo circular oscuro, nombre accesible y tooltip. No sidebar ancho con texto. Esta petición actualiza la preferencia anterior de no tener navegación lateral.
- Configuración, apariencia y ayuda se podrán incorporar a estas cápsulas cuando sus funciones existan. No mostrar enlaces privados ficticios en el acceso.
- Login minimalista: un solo bloque compacto, título breve y campos necesarios. /login representa ahora vinculación Telegram, no un formulario de usuario y contraseña del panel.

## 16. Perfil y conexión permanente implementados

La sesión MTProto cifrada ya se recupera al arrancar y mantiene un cliente conectado aunque no haya navegadores abiertos. Los fallos transitorios usan reconexión con espera; la recuperación completa de diferencias se añadirá junto al catálogo para no confundir conexión viva con sincronización fiable.

El perfil propio se guarda como una instantánea cifrada en PostgreSQL. Nombre, usuario y teléfono no quedan en texto plano; la miniatura de avatar se almacena separada para servirla con su tipo de contenido. `/dashboard` carga primero esta instantánea y muestra por separado si Telegram está conectado o reconectando.

El router consulta el estado de instalación antes de mostrar una pantalla: `/` resuelve el destino correcto, `/setup/database` solo permanece accesible mientras PostgreSQL falta o está en recuperación, `/login` solo durante la vinculación y `/home` solo con una sesión Telegram autorizada. Completar cada etapa reemplaza la ruta automáticamente para que atrás no devuelva al paso terminado.

`/home` sustituye a `/dashboard` como destino principal. Muestra los topics del grupo activo; los diagnósticos de PostgreSQL pertenecen a ajustes del sistema. La navegación superior contiene áreas globales y las cápsulas laterales contienen herramientas contextuales. `/dashboard` se conserva únicamente como redirección compatible a `/home`.
- Flujo real previsto: si faltan API ID/API hash, pedirlos; después teléfono con prefijo, código de Telegram y contraseña 2FA solo cuando el servidor lo requiera. Si existe una sesión Telegram válida, no volver a solicitar sus datos. Los códigos no necesariamente llegan por SMS: mostrar el método real informado por Telegram.
- Prototipo actual: pantallas API y teléfono únicamente, sin conexión ni persistencia. No simular código enviado ni éxito. Los datos de ejemplo de validación deben ser ficticios.
- La protección de acceso remoto al panel sigue siendo un requisito independiente; definir su pantalla/ruta separada al implementar autenticación. No confundir vincular Telegram con autorizar a cualquier visitante del dominio.

## 16. Desarrollo local y distribución final

Todas las pruebas actuales se realizan localmente en macOS. No configurar ni desplegar todavía en el home server. Al terminar y validar las funciones del producto se abordarán, por este orden:

1. Empaquetado nativo para Linux, macOS y Windows, con rutas de datos propias de cada sistema y pruebas reales en cada plataforma.
2. Contenedores Docker y stacks compatibles con Portainer, volúmenes persistentes, secretos y comprobaciones de salud.
3. Pruebas finales de LAN, Nginx Proxy Manager, dominio/TLS, WebSocket y streaming largo en el despliegue definitivo.
4. Instalación, actualización, respaldo y restauración documentadas para cada modalidad.

Desde ahora evitar dependencias de shell Unix en la aplicación, rutas absolutas del Mac y estado efímero. La compatibilidad multiplataforma se diseña ahora; no se da por verificada hasta probarla en esas plataformas. PostgreSQL es un servicio aparte también en modo nativo: la aplicación no instalará silenciosamente un servidor de base de datos en el sistema.

## 17. Configuración PostgreSQL desde la web

Ruta inicial: `/setup/database`. Campos: host, puerto, nombre, usuario, contraseña, modo TLS y ruta a CA cuando corresponda. La prueba de conexión no guarda datos ni aplica migraciones. Guardar vuelve a validar y prepara el esquema transaccionalmente antes de persistir la conexión cifrada. La base debe existir y el usuario debe tener permisos para crear el esquema TgStrm; no se exige superusuario.

La conexión mínima se almacena fuera de PostgreSQL porque se necesita antes de poder conectarse. En esta primera implementación vive en `data/database.enc` con AES-GCM y clave aleatoria persistente `data/master.key`; `data/setup.key` protege el asistente. No se almacenan en el frontend ni se devuelven secretos por la API. Respaldar configuración y clave juntas de forma protegida; el cifrado no protege frente a quien tenga acceso a ambos archivos. No reemplazar una clave ausente en una instalación existente.

Las tablas viven en esquema dedicado `tgstrm`; el historial de migraciones conserva nombre y SHA-256 del SQL aplicado. Bloqueo transaccional PostgreSQL evita ejecutar simultáneamente migraciones. Rechazar historial desconocido o modificado. Arrancar con conexión guardada inaccesible o incompatible produce estado de recuperación, nunca un nuevo bootstrap.

La edición posterior requerirá autenticación del panel, comprobación de identidad de instalación y modo mantenimiento. Cambiar la dirección de la misma base y trasladar datos a otra base son operaciones diferentes; no tratar Guardar como una migración de datos. El asistente inicial se bloquea después de configurar. Su token no sustituye las sesiones administrativas futuras.

Implementación actual limitada a desarrollo local: el acceso MTProto de Telegram está conectado a la API, pero aún debe validarse con una cuenta real. Siguen pendientes el gestor de usuarios, la edición de conexiones configuradas, la carga de `.env`, los ajustes operativos y la protección remota completa. El lector de configuración distingue valores ausentes de archivos inválidos.

El flujo Telegram persiste tres piezas cifradas y separadas por contexto criptográfico: API ID/hash, estado temporal del desafío y sesión MTProto. El teléfono y `phone_code_hash` viven solo durante el desafío; el código introducido y la contraseña 2FA no se persisten. El desafío caduca y los reenvíos se limitan antes de llamar a Telegram. Al reiniciar, la interfaz recupera el paso pendiente o verifica la sesión almacenada. Las credenciales API pueden corregirse antes de autorizar la cuenta; hacerlo descarta sesión y desafío incompletos. Una cuenta ya autorizada no puede sustituirse desde este asistente.

La implementación usa un cliente corto durante el acceso. El cliente persistente, el motor de recuperación de updates y la reconexión continua pertenecen a la etapa de catálogo; no deben añadirse como efecto secundario del formulario.

Fuentes oficiales consultadas para esta etapa:
- PostgreSQL y TLS: https://www.postgresql.org/docs/current/libpq-connect.html
- pgx, controlador Go: https://github.com/jackc/pgx
- Telegram, autorización: https://core.telegram.org/api/auth
- Telegram, API ID: https://core.telegram.org/api/obtaining_api_id
- gotd, almacenamiento de sesiones: https://ref.gotd.dev/src/github.com/gotd/td/session/session.go.html

El acceso antiguo de `old/app.py` usa `send_code_request`, `phone_code_hash` y `SessionPasswordNeededError`. La implementación nueva conservará el challenge en el servidor y no trasladará la sesión Telethon existente ni repetirá envíos automáticamente ante cualquier error. Los códigos y la contraseña 2FA no se persistirán como ajustes.
