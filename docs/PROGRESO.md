# Progreso de TgStrm

Se marca terminado solo lo implementado y verificado. La conformidad visual del usuario se registra por separado.

## Paso 1 — Estructura y primera revisión de acceso
- [x] Arquitectura documentada y píldoras superiores y cápsulas laterales compactas de iconos según referencia.
- [x] Home antiguo revisado y distribución de funciones documentada.
- [x] Fuentes Stara locales recuperadas.
- [x] Base Go separada de frontend React + TypeScript + Vite.
- [x] Ruta /login simplificada para Telegram: API ID/API hash y teléfono; diseño neutro.
- [x] Compilaciones verificadas: TypeScript/Vite, Go y go vet.
- [x] Revisión visual de la versión neutra en escritorio y transición API → teléfono con datos ficticios.
- [x] Revisión móvil del acceso y del shell privado; sin desbordamiento horizontal.
- [x] Usuario conforme con el diseño de acceso: estética minimalista neutra aprobada como línea visual del proyecto.

La pantalla de acceso usa el flujo real de Telegram y conduce al inicio cuando la sesión está autorizada.

## Paso 2 — Acceso real y primera instalación
- [x] PostgreSQL: asistente web para probar conexión y guardarla cifrada sin .env obligatorio.
- [x] Migraciones transaccionales con bloqueo y checksum; esquema dedicado.
- [x] Tests con PostgreSQL temporal: reinicio, datos preservados, migración repetida y rechazo de historial alterado.
- [x] Tests de configuración cifrada: reinicio, manipulación y pérdida de clave.
- [x] Asistente protegido por código de instalación y comprobación de origen; no sustituye el futuro login administrativo.
- [ ] Edición autenticada de conexión existente y proceso de traslado de datos.
- [ ] Administrador, login, logout y sesiones persistentes.
- [ ] Protección de primera instalación, límites de intentos y CSRF.
- [x] Asistente web de vinculación Telegram implementado: API, teléfono, código y 2FA condicional.
- [x] Revocación detectada como estado operativo: sesión local retirada, procesos MTProto pausados, incidencia registrada y redirección web al acceso.
- [x] Cierre normal mediante `auth.logOut`, confirmación visual y nueva vinculación por teléfono, código y 2FA; catálogo, STRM, ajustes y colas se conservan.
- [x] Reautorización reactiva el cliente de updates y reanuda importaciones y entregas persistentes sin regenerar el catálogo.
- [x] Flujo MTProto implementado: API ID/hash, teléfono, código, 2FA y detección de sesión autorizada.
- [x] Credenciales, challenge y sesión MTProto cifrados; código y contraseña 2FA no persistidos.
- [x] Interfaz conectada a los estados reales del servidor y recuperación del paso tras recargar.
- [x] Vinculación con una cuenta Telegram real y comprobación de sesión tras reiniciar.
- [x] Cliente MTProto permanente con reconexión y estado de conexión real.
- [x] Perfil propio cifrado en PostgreSQL; nombre, teléfono y foto actualizados desde Telegram.
- [x] Ruta `/dashboard` con píldoras superiores/laterales y adaptación móvil.
- [x] Shell corregido: todas las píldoras flotan fuera del `dashboard-frame`, reservado exclusivamente para contenido.
- [x] Enrutamiento automático por estado: base pendiente → configuración, Telegram pendiente → acceso, instalación lista → inicio.
- [ ] Logout/revocación y autenticación administrativa para habilitar acceso remoto.

## Paso 3 — Catálogo y sincronización
- [x] Fundación normalizada para chats, topics y estados de sincronización mediante migración 004.
- [x] Ingesta MTProto paginada de grupos/canales con upserts y actualización periódica en el servidor.
- [x] Ingesta de topics bajo demanda con paginación MTProto, reconciliación transaccional y estado por grupo.
- [x] Gestor oficial de updates de gotd conectado, con estado global/canales persistido y recuperación de diferencias.
- [x] Outbox transaccional y WebSocket con reconexión y recarga del snapshot al recuperar conexión.
- [x] Ruta estable `/groups/:groupId`, detalle de grupo y API de topics.
- [x] `/home` sustituye `/dashboard` y muestra el catálogo real de grupos.
- [x] Home con tarjetas navegables; cada grupo mantiene su propia URL estable.
- [x] Topics reales y búsqueda dentro de la página de cada grupo.
- [x] Ruta estable `/groups/:groupId/topics/:topicId` y navegación al detalle de un topic.
- [x] Primera página de archivos de vídeo obtenida con `messages.getReplies`, persistida en PostgreSQL y paginación hacia el historial.
- [x] Filtro estricto de vídeos en la ingesta, la consulta y la interfaz; los mensajes normales no se almacenan.
- [x] Modelo preparado para remitentes, ediciones, respuestas, álbumes y metadatos de archivos.
- [x] Ruta estable `/groups/:groupId/topics/:topicId/videos/:videoId` y ficha individual con metadatos públicos.
- [x] Datacenter de Telegram persistido internamente para preparar descarga por fragmentos sin exponer credenciales al frontend.
- [x] Endpoint de streaming con `HEAD`, un único rango HTTP, respuestas `206/416` y fragmentos Telegram alineados.
- [x] Saltos reales verificados al inicio y a 800 MB de un vídeo de 1,56 GB sin descargar el archivo completo.
- [x] Renovación automática de `file_reference` caducado desde el mensaje original, persistencia transaccional y un solo reintento.
- [x] Caché efímera LRU de fragmentos (64 MiB/5 min) y deduplicación de solicitudes concurrentes.
- [x] Reproductor HTML5 responsive conectado al streaming por rangos, con estados de carga, buffering, error y reintento.
- [x] Respuesta progresiva HTTP `200` sin `Range` para compatibilidad estricta con Safari, manteniendo `206` para saltos parciales.
- [x] Rangos grandes de Safari recompuestos desde fragmentos MTProto, conservando exactamente `Content-Range`, `Content-Length`, `ETag` e `If-Range`.
- [x] Saltos estabilizados mediante respuestas `206` acotadas a 8 MiB: las peticiones abandonadas ya no monopolizan la cola MTProto; verificado en HTML5 con un salto de 2,6 s a 1:16:53 y reproducción posterior continua.
- [x] Transferencias separadas del canal de updates mediante pool MTProto dedicado por DC: cuatro conexiones, calentamiento previo y reserva efectiva para saltos prioritarios.
- [x] Compatibilidad por DC: los rechazos `DC_ID_INVALID` usan el enrutamiento probado de gotd y quedan en enfriamiento diez minutos, evitando reintentos de autorización y `FLOOD_WAIT` que retrasaban otros vídeos.
- [x] Entrega continua detrás de Nginx Proxy Manager con buffering desactivado, flush por fragmento y bloques MTProto de hasta 1 MiB.
- [x] Buffering gestionado internamente por el elemento multimedia sin cubrir una reproducción que continúa visible en Safari.
- [x] Ficha de vídeo en cuadrícula responsive y reproductor dimensionado con la proporción real de cada archivo.
- [x] Composición ultrawide a ancho y alto completos: reproductor limitado por altura y datos fluidos ocupando el espacio restante.
- [x] Cinco datos adaptables en la parte superior lateral y pie extendido hasta la base del reproductor; regreso redundante eliminado.
- [x] Crecimiento del reproductor calculado por altura disponible y proporción real, sin límite fijo en pantallas grandes.
- [ ] CDN para despliegues que lo necesiten.
- [x] Updates de vídeos nuevos y editados conectados a reconciliación MTProto, PostgreSQL, outbox y WebSocket.
- [x] Aplicación directa de updates completos de vídeo sin RPC adicional; reconciliación reservada para huecos, topics desconocidos y reconexiones.
- [x] Eliminaciones y conversiones de vídeo a contenido no válido desactivadas en PostgreSQL y notificadas al topic abierto.
- [x] Recuperación tras desconexión mediante estado persistente de updates y reconciliación al reconectar el WebSocket.
- [x] Recuperación explícita para `differenceTooLong`, pérdida de estado global/canal y canales inaccesibles.
- [x] Concurrencia de diferencias de canales limitada para evitar ráfagas contra Telegram.
- [x] Grupos inaccesibles retirados transaccionalmente y propagados al home mediante WebSocket.
- [ ] Favoritos.

## Paso 4 — Operación
- [ ] Trabajos, logs, actividad y ajustes.
- [ ] Backups y restauración comprobada.
- [x] Panel de Trabajos construido sobre los estados persistentes de importaciones, Espejo, STRM y sincronizaciones, sin duplicar colas ni procesos.
- [x] Historial de Espejo agrupado en Actividad y Trabajos: una operación por vínculo, detalle desplegable, incidencias primero y progreso persistente.
- [x] Auditoría manual de coherencia entre PostgreSQL y STRM, con reparación limitada a exportaciones activas ausentes mediante escritura atómica.
- [x] Eventos WebSocket de instantáneas descartados de forma segura cuando no hay pestañas conectadas; la interfaz vuelve a leer el estado canónico al abrirse.

## Paso 5 — Biblioteca
- [ ] Streaming, STRM y TMDB.
- [x] Identidad pública UUID permanente por vídeo y endpoint `/stream/{uuid}` independiente de la sesión web y referencias temporales de Telegram.
- [x] Modelo normalizado para destinos de biblioteca, reglas por topic y registro idempotente de exportaciones.
- [x] Ajustes de medios desde la web: URL pública, destinos separados de películas/series y credencial de TMDB cifrada sin devolución al frontend.
- [x] Destinos predeterminados `data/library/movies` y `data/library/shows`, preparados para montar `/data` como volumen en Docker/Portainer.
- [x] Validación de API Key v3 o Read Access Token v4 contra TMDB con estado guardado/verificado independiente.
- [x] Grupo principal configurable desde la web y persistido como catálogo prioritario.
- [x] Paginación completa por `offset_id`: páginas locales desde PostgreSQL y consulta a Telegram solo al agotar el tramo persistido.
- [x] Enriquecimiento TMDB en segundo plano con coincidencia conservadora por título y año, progreso visible y póster persistente por película.
- [x] Vista de películas en cuadrícula: cuatro columnas en escritorio, tres en tablet y dos en móvil.
- [x] Portadas de topics: backdrop horizontal TMDB para series y collage estable de películas identificadas para topics de colección.
- [x] Vista normal del grupo limpia y gestión contextual separada en `/groups/:groupId/manage` desde la píldora lateral.
- [x] Modo de selección de topics con selección individual, todos los resultados visibles y limpieza completa.
- [ ] Revisión previa de coincidencias TMDB y rutas antes de crear un trabajo de exportación.
- [x] Importaciones persistentes en PostgreSQL: manifiesto completo antes de responder, ejecución secuencial, progreso, reintento y recuperación tras reinicios sin una segunda cola.
- [x] Generación manual STRM por archivo, topic completo o selección de topics; registro idempotente en PostgreSQL y renombrado atómico.
- [x] Proxy público `/stream` corregido para entregar vídeo en lugar del HTML de Vite; verificado mediante HEAD, Range y ffprobe sobre HTTPS.
- [x] Inicio adaptativo: primer bloque de 256 KiB para reducir tiempo al primer byte y prelectura ordenada de hasta tres bloques MTProto.
- [x] Caché efímera normalizada en bloques de 256 KiB: reutiliza rangos solapados de VLC, Jellyfin y Safari.
- [x] Preparación en segundo plano de la ruta al DC multimedia y de los extremos del MP4 tras conectar Telegram.
- [x] Caché multimedia persistente bajo `data/cache/telegram-blocks`: escritura atómica, validación, recuperación tras reinicios y expulsión LRU.
- [x] Ajustes web de caché con uso real, número de bloques, límite configurable de 64 MB a 8 GB y vaciado de disco y memoria.
- [x] Lectura estructural de cajas MP4: salto directo sobre `mdat`, detección de `moov` al principio o al final y precarga deduplicada de su rango completo.
- [x] Política selectiva de persistencia: primer MiB e índice MP4 únicamente; reproducción y saltos intermedios limitados a la caché efímera de memoria.
- [x] Prueba de cierre/reinicio: rango inicial reabierto desde disco en 43 ms; lectura de 8 MiB en mitad del vídeo sin crecimiento de caché persistente.
- [x] Verificación real sobre HTTPS: índice `moov` de 5.936.007 bytes detectado sin recorrer un vídeo de 2,48 GiB; inspección con ffprobe reducida de ~5,5 s a ~1,3 s.
- [x] Modo Gestión conservado al retroceder por el catálogo, sin introducir un paso artificial en el historial.
- [x] Comparativa real sobre HTTPS: 8 MiB mejoraron de 3,60 s secuenciales a 3,25 s con prelectura limitada; HTML5 conserva duración y `readyState=4`.
- [x] Saltos fríos medidos con el pool dedicado: primer byte en 0,79 s, 0,30 s y 0,16 s; tras cinco cancelaciones consecutivas, el siguiente salto respondió en 0,86 s y continuó reproduciendo.
- [x] Regresión del DC 4 corregida y comparativa posterior del vídeo de 2,48 GiB: primeros bytes de tres saltos en 0,36 s, 0,22 s y 0,25 s; reproducción visual confirmada tras saltar a 30:36 en un archivo del DC 4.
- [x] URL pública fijada en la primera generación para impedir que accesos posteriores desde localhost cambien enlaces existentes.
- [ ] NPM, HTTPS, LAN y enlaces externos verificados.

Acceso provisional de desarrollo:
- [x] Frontend disponible en LAN mediante una única entrada web; Go permanece en loopback.
- [x] Proxy HTTP y WebSocket preparados para Nginx Proxy Manager con lista explícita de hosts.
- [ ] Subdominio y certificado reales verificados en NPM.
- [ ] Sustituir la protección provisional de NPM por autenticación administrativa propia antes de producción.

## Paso 6 — Automatizaciones
- [x] Módulo Espejo con navegación propia, listado y gestión responsive de vínculos entre topics.
- [x] Reglas persistentes y entregas idempotentes recuperables después de reinicios.
- [x] Nuevos vídeos reflejados al topic principal mediante updates MTProto, reintentos y reconciliación de huecos.
- [x] Progreso del espejo propagado por outbox/WebSocket y actualizado en la interfaz.
- [x] Ediciones del pie reflejadas sobre el mismo mensaje, con ID de destino persistente, reintentos y recuperación de vínculos anteriores.
- [x] Importación opcional del historial al crear un vínculo: catálogo completo, entregas idempotentes persistentes, progreso y continuidad en tiempo real.
- [x] Prueba real de historial con ALF: 99 de 99 archivos adoptados sin duplicados, recuperación tras reinicio e idempotencia al repetir la solicitud.
- [x] Refrescos WebSocket del topic servidos desde PostgreSQL para evitar repetir consultas MTProto y provocar `FLOOD_WAIT`; la entrada inicial conserva la reconciliación remota.
- [ ] Política explícita para propagar eliminaciones del origen.

## Paso 7 — Distribución, al terminar las funciones
- [ ] Ejecución nativa Linux, macOS y Windows.
- [ ] Docker y stacks Portainer con volúmenes persistentes.
- [ ] Validación final LAN, NPM y HTTPS en home server.
- [ ] Guías de instalación, actualización y restauración.

Estado detallado y verificable: [ENTREGA_Y_DISTRIBUCION.md](ENTREGA_Y_DISTRIBUCION.md). La base de empaquetado, protección de secretos, binario con interfaz embebida, plantillas Docker/Portainer y workflows de release están preparados; faltan las pruebas reales de Docker, NPM y los artefactos que publicará el primer tag.

## Respaldo y recuperación
- [x] Separación explícita entre raíz persistente de TgStrm y destinos externos STRM.
- [x] Copia manual consistente mediante `pg_dump` personalizado, manifiesto con SHA-256 y configuración cifrada de arranque.
- [x] Creación y listado de copias desde Configuración; caché y bibliotecas STRM quedan excluidas.
- [ ] Descarga protegida después de incorporar autenticación administrativa propia.
- [x] Restauración local validada por identidad y SHA-256, con copia preventiva, PostgreSQL transaccional y modo mantenimiento.
- [x] Programación diaria, semanal o por intervalo, ejecutada por el servidor y persistente tras reinicios.
- [x] Política de retención configurable y resultados integrados en Actividad y Notificaciones.

Desarrollo y pruebas actuales: solo macOS local.

## Estado local de desarrollo

- [x] Base dedicada `tgstrm_dev` creada en el PostgreSQL local de macOS.
- [x] Conexión guardada desde el asistente y migraciones 001/002 aplicadas.
- [x] API reiniciada y configuración PostgreSQL recuperada sin repetir el bootstrap.
- [x] Cuenta real conectada; perfil y foto recuperados después de reiniciar la API.
