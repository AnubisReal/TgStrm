# Auditoría profunda del servicio TgStrm

Fecha: 2026-09-26

## Alcance

Se revisan todos los archivos ejecutables de cmd/ y internal/, la persistencia, HTTP, WebSocket, Telegram/MTProto, streaming, cachés, imports, espejos, TMDB, STRM, autenticación, backups y las migraciones 001-036. Los tests se usan como evidencia de cobertura. La auditoría no cambia datos de Telegram, PostgreSQL ni bibliotecas.

Las decisiones son: mantener, mejorar o replantear. DB significa PostgreSQL como estado canónico; Telegram es la fuente remota; filesystem es una proyección reparable.

## Veredicto general

La arquitectura es válida y no necesita reescribirse. PostgreSQL, outbox, WebSocket, workers persistentes y STRM estables son la dirección correcta. La prioridad es hacer idempotentes todas las colas y dejar inequívoca la relación entre DB, archivos STRM y mensajes Telegram.

Prioridades:

1. Idempotencia y leases en importaciones, espejos, scans y generación STRM.
2. DB primero: el filesystem debe poder reconstruirse sin duplicar ni borrar carpetas compartidas.
3. Restauración probada en cada release, no solo creación de backups.
4. Backoff, límites y observabilidad para Telegram y TMDB.
5. Endurecimiento de streaming público, cookies, proxy y recuperación de cuenta.
6. Pruebas de reinicio, duplicados, edición de captions, borrado de topics y dos instancias simultáneas.

## Inventario y valoración por función

### Arranque, configuración y PostgreSQL

| Función | Responsabilidad y recomendación |
|---|---|
| cmd/tgstrm.main | Arranca configuración, API y shutdown. Mejorar separando bootstrap, lifecycle y señales para probarlos aisladamente. |
| cmd/tgstrm.runHealthcheck | Comprueba disponibilidad. Mantener con timeout y estados readiness/liveness separados. |
| Store.Dir | Devuelve raíz persistente. Mantener. |
| Store.EnsureDataLayout | Crea data, cache, library y backups. Mejorar permisos, fsync y rechazo de rutas iguales. |
| config.Open | Inicializa clave y configuración cifrada. Mejorar bloqueo para evitar dos procesos usando la misma raíz. |
| readOrCreate | Lee/crea secretos. Mantener y probar permisos, disco lleno y corrupción. |
| Store.aead | Construye AEAD. Mantener; documentar rotación y pérdida de master.key. |
| Store.LoadDatabase | Descifra PostgreSQL. Mejorar diferenciando ausente, corrupto e incompatible. |
| Store.SaveDatabase | Valida, cifra y persiste. Mejorar escritura atómica y fsync. |
| Store.Seal / Unseal | Cifra secretos por ámbito. Mantener; verificar separación de ámbitos. |
| postgres.Validate | Valida conexión. Mantener con límites y sin registrar contraseñas. |
| postgres.Connect | Crea pool pgx y hace Ping. Mejorar timeout configurable y healthcheck separado. |
| postgres.Migrate | Ejecuta migraciones con advisory lock/checksum. Mantener; rechazar siempre checksums desconocidos. |
| reconcileLegacyMigrationChecksum | Repara solo la migración histórica conocida después de comprobar las cinco columnas. Mantener con prueba de restore. |

### Router, HTTP, WebSocket y seguridad

| Función | Responsabilidad y recomendación |
|---|---|
| New / NewWithOptions | Composición de API y workers. Mejorar lifecycle explícito y cancelación completa. |
| API.Close | Cierra dependencias. Mejorar idempotencia y esperar workers antes de DB. |
| writeJSON / fail | Respuestas consistentes. Mantener; añadir request_id y ocultar detalles internos. |
| API.Handler | Registra rutas/middleware. Mejorar autorización por recurso y límites de body. |
| parseSingleRange | Analiza un rango HTTP. Mantener; rechazar overflow y rangos múltiples. |
| videoStream / publicVideoStream | Admin stream autenticado; ruta `/stream/{assetID}` pública por compatibilidad `.strm`, con UUID persistente como URL-capacidad. Mantener, explicar amenaza y validar política de exposición. |
| serveVideoLocation / videoFullStream / writeVideoBytes | Descarga y escribe rangos Telegram. Mejorar backpressure, cancelación y límite de concurrencia. |
| video | Metadatos de vídeo. Mantener con autorización. |
| watchTopic / topicMessages | Suscripción y lectura paginada. Mejorar TTL de watchers y cursor estable. |
| telegramService | Acceso al servicio Telegram. Mejorar usando interfaz mínima. |
| group / groupTopics / groups | Grupos y topics. Mejorar ETags, paginación y no refrescar Telegram en cada lectura. |
| events | Entrega WebSocket. Mantener con ping/pong, límite por cliente lento y backpressure. |
| selectGroup | Selecciona grupo principal. Mantener validando existencia. |
| profile / avatar | Perfil y avatar. Mejorar ETag y límite de tamaño. |
| sameOrigin | Comprueba Origin/Host y permite cualquier dominio NPM del mismo origen. Mantener; probar proxy. |
| telegramAuth / telegramLogout | Login/logout Telegram. Mejorar rate limit y respuestas uniformes. |
| protect / decodeDatabase / testDatabase / saveDatabase | Bootstrap de DB. Mantener con claves de un uso, expiración y limpieza de contraseñas en memoria. |

### Cuenta administrativa

| Función | Responsabilidad y recomendación |
|---|---|
| randomBytes / tokenHash | Tokens criptográficos. Mantener. |
| hashPassword / verifyPassword | Hash de contraseña. Mantener; revisar coste Argon2id periódicamente. |
| ensureDefaultAdmin | Crea admin inicial. Replantear antes de exposición: secreto aleatorio o cambio obligatorio antes de acceso remoto. |
| validNewPassword | Política de contraseña. Mejorar con longitud razonable y bloqueo de contraseñas comprometidas. |
| adminSkipInitialPassword | Salta protección inicial. Replantear: solo loopback/bootstrap protegido. |
| parseTrustedProxyCIDRs / requestSecure | Confía en HTTPS reenviado desde CIDR. Mantener; nunca confiar en cualquier cliente. |
| sessionCookieName / setSessionCookie / clearSessionCookies | Cookies. Mantener Secure, HttpOnly, SameSite, rotación y revocación. |
| readSession / createSession | Sesiones persistentes. Mejorar hash en DB, expiración absoluta, límite y revocación. |
| decodeAdminJSON | JSON administrativo. Mantener con límite de body y campos desconocidos rechazados. |
| adminSessionStatus / adminLogin / adminInitialPassword / adminLogout | Flujos de acceso. Login limita fallos por cuenta; falta telemetría estructurada y rate limit distribuido. |
| recoveryCode / adminRecoveryRequest / adminRecoveryComplete | Código hasheado, un uso, expiración, límite de solicitud por cuenta y cinco intentos; el código va al log local. Restringir acceso a esos logs. |
| adminAccount / adminSessions | Cuenta y sesiones. Mantener; reautenticación para acciones sensibles. |
| adminGate / securityHeaders | Middleware. Mantener; CSP, HSTS solo bajo HTTPS y política compatible con TMDB/WebSocket. |

### Telegram, catálogo, topics y mensajes

| Función | Responsabilidad y recomendación |
|---|---|
| syncDialogs / RefreshDialogs | Sincronizan grupos. Mejorar batches, backoff y flood-wait metrics. |
| IndexRemoteTopic | Indexa topic externo. Mantener con clave peer/topic única. |
| Groups / SelectGroup | Lista y selecciona grupo. Mantener validando permisos. |
| Group / RefreshTopics / Topics | Topics y refresh. Separar lectura de refresh explícito y guardar cursor. |
| RefreshSearchTopics / RefreshSearchMedia | Búsqueda remota. Replantear fan-out ilimitado; cola y rate limit. |
| recordTopicSyncFailure | Registra fallos. Mantener con clasificación transitorio/permanente. |
| peerIdentity | Identidad de peer. Mantener y probar todos los tipos. |
| isVideoMedia / videoMetadata | Detecta vídeo directo o documento. Mantener y ampliar matriz MIME/extensiones. |
| Topic / RefreshTopicMessages | Topic y páginas remotas. Mantener con cursor y deduplicación. |
| RefreshTopicVideos / RefreshTopicVideoDocuments | Dos fuentes de vídeos. Mantener compartiendo almacenamiento/conteos. |
| SyncTopicVideoCatalog / refreshTopicFilteredVideos | Reconcilian catálogo. Replantear como operación reanudable por lotes. |
| storeVideoMessage | Upsert de mensaje. Crítica: índice único remoto y asociación estable STRM. |
| Video / TopicMessages | Lectura local. Mantener sin consultar Telegram en cada render. |
| globalPeerKey / strconvInt64 | Helpers de búsqueda. Mantener con pruebas de colisión. |
| GlobalSearch | Busca Telegram. Mejorar límites, cancelación, cache corta y rate limit. |
| DeleteVideoMessage / DeleteForumTopic | Borrado remoto. Replantear confirmación, auditoría y separación de limpieza local. |

### Realtime y reconciliación

| Función | Responsabilidad y recomendación |
|---|---|
| WatchTopic / reconcileOneWatchedTopic / reconcileWatchedTopicPresence | Watchers y reparación. Mantener con TTL/backoff. |
| Subscribe / deliverOutbox | Outbox y entrega. Crítica: lease para evitar doble entrega entre workers. |
| queueTopicRefresh / queueVideoRefresh | Encola updates. Mantener con debounce y deduplicación. |
| queueChannelRecovery / queueCatalogRecovery | Recuperación. Mantener con jitter y backoff. |
| queueInaccessibleChannel / markChannelInaccessible | Canal inaccesible. Mejorar diferenciando permisos, borrado y error temporal. |
| topicFromMessage / channelFromPeer | Relación update-topic/canal. Mantener con cobertura completa. |
| queueVideoEvent / applyRealtimeVideo | Aplica update a DB/outbox. Crítica: una transacción para mensaje, exportación pendiente y evento. |
| refreshVideosForTopic / refreshAllVideosForChannel / refreshTopicsForChannel | Reconciliación. Mejorar checkpoints para no repetir historial entero. |
| reconcileOneUnresolvedPrimaryTopic / recoverCatalogState | Recuperación tras reinicio. Mantener como job visible. |
| removeDeletedVideos / removeDeletedTopics | Propaga borrados remotos. Replantear política: distinguir origen borrado de destino conservado. |

### Streaming, caché y MP4

| Función | Responsabilidad y recomendación |
|---|---|
| warmStreamRoute | Calienta rutas. Mejorar evitando descargas sin demanda. |
| VideoLocation / PublicVideoLocation / videoLocation | Resuelve referencias. Crítica: expiración y token revocable sin secretos en URL. |
| DownloadVideoRange / downloadPersistentVideoRange / downloadVideoRange | Rangos y caché. Mantener con límites de memoria/concurrencia. |
| shouldPersistStreamBlock | Decide persistencia. Mantener y medir hit-rate real. |
| PersistentCacheStats / SetPersistentCacheLimit / ClearPersistentCache | Gestión caché. Mejorar operación atómica, cuota por recurso y aviso. |
| sliceStreamBlock | Recorte de bloque. Mantener con límites/overflow. |
| downloadVideoRangeWithRefresh / refreshExpiredVideoLocation | Renovación y retry único. Mantener. |
| cachedStreamRange / beginStreamCall / finishStreamCall | Deduplicación concurrente. Crítica: cancelación y timeout del líder. |
| evictStreamCacheLocked | LRU/TTL. Mantener sin expulsar entradas en uso. |
| downloadVideoRangeOnce | Descarga MTProto concreta. Mejorar retry solo transitorio y métricas. |
| parseMP4BoxHeader / PrepareVideoIndex / findMP4Index | Índice MP4 y seeks. Mantener bajo demanda/cacheado. |
| readVideoBytes / warmMP4Index | Lectura e indexado. Mejorar workers limitados y no bloquear reproducción. |

### Imports, espejos y colas

| Función | Responsabilidad y recomendación |
|---|---|
| importEpisodeOrder / sortImportDocuments | Ordena documentos. Mantener orden remoto como desempate. |
| stableRandomID | Correlación determinista. Mantener documentando dominio y colisiones. |
| publishImportProgress | Progreso persistente. Mantener agregado por lote. |
| ResumeCatalogImports / RetryCatalogImport | Reanuda/reintenta. Crítica: lease, máximo de intentos y estado terminal único. |
| ImportTopic / ImportTopicPrepared / ImportQueuedTopicPrepared / ImportQueuedTopicPreparedToTopic | API de importación. Mejorar centralizando idempotencia. |
| importTopicPrepared | Copia ordenada, caption y correlaciones. Crítica: clave origen-destino-mensaje y commit por mensaje. |
| mirrorLinks / createMirrorLink / mirrorLink | CRUD de espejos. Mantener unicidad y rechazar origen=destino. |
| RecoverMirrorSources | Recupera espejos. Mantener backoff. |
| QueueMirrorDelivery / QueueMirrorEdit | Cola de copia/edición. Crítica: outbox e idempotency key. |
| wakeMirrorWorker / WakeMirrorWorker | Despierta worker. Mantener no bloqueante. |
| ProcessMirrorDeliveries | Entrega espejo. Crítica: lease, orden por vínculo y dead-letter. |
| reconcileMirrorExistingDestinations / prepareMirrorHistory / refreshMirrorHistoryState | Historial espejo. Mejorar checkpoints/concurrencia. |
| publishMirrorHistoryProgress | Progreso histórico. Mantener agregado. |
| reconcileMirrorEditState / processMirrorEdits | Ediciones. Crítica: edición antes del mapping destino. |
| messageIDFromUpdates | Mapea mensaje creado. Mantener fallback con métricas. |
| failMirrorEdit / failMirror | Fallos de espejo. Mejorar categoría y retry visible. |
| enqueueCatalogImport / runCatalogImportQueue / processNextCatalogImport | Cola de import. Crítica: lease multiinstancia, FIFO por destino y deduplicación. |
| failCatalogImport / publishCatalogImportEvent | Terminaliza/notifica. Mantener transición única. |

### STRM, TMDB y catálogo

| Función | Responsabilidad y recomendación |
|---|---|
| safeLibraryName / movieSTRMPath | Sanitiza y crea ruta de película. Mantener con traversal y nombres reservados. |
| shouldReplaceEpisode | Decide reemplazo por fecha/ID. Mantener regla explícita. |
| atomicWrite | Escritura atómica. Mantener; fsync para instalaciones críticas. |
| removeEmptyExportParents / removeExportDirectoryMetadata | Limpia vacíos/metadata. Mantener, limitado a raíz y ownership. |
| removeStaleAssetExports / removeAssetExportsForMediaType | Borra STRM obsoletos. Crítica: ownership y dry-run antes de carpeta. |
| replaceEpisodeExport / completeEpisodeReplacement | Reemplazo de episodio. Mejorar rollback lógico filesystem+DB. |
| generateTopicSTRM | Endpoint manual. Mantener como job 202. |
| tmdbSource / normalizedTMDBTitle / tmdbExactMatch | Matching. Mejorar explicación de score y confirmación en ambigüedad. |
| startTMDBScan / runMovieMatchReconciler / processPendingPrimaryMovies / runTMDBMovieScan | Scans TMDB. Crítica: rate limit, cursor y no repetir finalizados. |
| tmdbScanStatus | Estado de scan. Mantener estados terminales/error. |
| splitTMDBIDMarker / splitTMDBTitleYear | Formatos heredados. Mantener con Unicode/títulos largos. |
| tmdbToken / normalizeTMDBType | Token y tipo. Mantener sin devolver token. |
| tmdbRequest / tmdbRequestContext | Cliente TMDB con timeout HTTP de 12 s. Añadido ritmo global por instancia y pausa tras HTTP 429; faltan métricas y una política durable para varios procesos. |
| searchTMDB / confirmTMDBMatch / listTMDBMatches | API TMDB. Mantener con autorización/auditoría. |
| startTopicArtworkScan / launchTopicArtworkScan / runTopicArtworkReconciler / runTopicArtworkScan | Posters/backdrops. Mejorar cache y estado sin coincidencia. |
| saveTopicTVMatch / topicArtwork | Persistencia artwork. Mantener con ETag. |
| detectEpisode / seriesSTRMPath / tmdbEpisodeTitle | Episodio y ruta. Crítica: conservar evidencia filename/caption y marcar ambiguos. |
| episodeResolutionScope / episodeResolution / seasonEpisodes / saveEpisodeResolution | Resolución manual. Mantener confirmación para cambiar publicado. |
| catalogTopicTitle / localizedTVDetails / localizedMovieDetails | Títulos localizados. Mantener título original + localizado + ID. |
| matchExternalSeries / searchExternalSeries / matchExternalMovie / searchExternalMovies | Preview de import. Mejorar evitando auto-confirmación de baja confianza. |
| importDestinations | Destinos. Mantener validación y autorización. |
| catalogSearch / indexRemoteSearchResult | Búsqueda local/remota. Mejorar full-text, paginación y debounce. |

### Actividad, trabajos, reconciliación, configuración y backups

| Función | Responsabilidad y recomendación |
|---|---|
| activity / clearActivity | Actividad agrupada y limpieza. Mejorar retención y énfasis de errores. |
| jobs / retryJob | Trabajos y retry. Mantener idempotencia y permisos. |
| reconciliation / repairMissingExports | Detecta/repara STRM. Mantener dry-run y locks. |
| deletedTopicTrees | Agrupa árboles eliminados. Mantener ownership y protección de raíz. |
| runExportReconciler / retryEpisodeReplacements / reconcileCompletedCatalogImports | Workers. Crítica: scheduler único, locks y métricas. |
| autoGeneratePrimarySTRM / generateAutomaticTopicSTRM / generateSelectedTopicSTRM | Generación automática. Mejorar cola durable y deduplicación por asset. |
| reconcileExports / publishExportChange | DB, disco y eventos. Crítica: DB primero y filesystem reconstruible. |
| mediaSettingsDefaults / mediaSettings | Rutas, TMDB, cache y notificaciones. Mantener validación de raíces. |
| clearMediaCache / testTMDB | Cache y conectividad. Mantener con progreso. |
| defaultBackupPolicy / backups / backupSettings | Política/listado. Mantener cuotas y path traversal. |
| createBackup / writeBackupArchive / fileSHA256 | Genera dump custom, manifiesto y sumas. Comprueba el dump con `pg_restore --list`; quedan pendientes fsync del directorio y evaluar cifrado en reposo. |
| loadBackupSettings / saveBackupSettings / nextBackupRun | Agenda. Mantener zona horaria explícita. |
| runBackupScheduler / runScheduledBackup | Scheduler. Mejorar lease y shutdown. |
| pruneBackups / listBackups / readBackupManifest | Retención y validación. Mantener siempre un backup válido. |
| restoreBackup / prepareRestore / extractRestoreArchive | Extrae a staging, exige entradas regulares permitidas y verifica las sumas antes de restaurar. Límite: falta matriz de recuperación ante caída del proceso. |
| runPGRestore / replaceBootstrapFiles | `pg_restore --single-transaction`; reemplazo de bootstrap con staging y rollback ante errores detectados. Si falla el reemplazo, vuelve a restaurar PostgreSQL desde la copia preventiva. No hay atomicidad conjunta ante corte eléctrico. |
| systemHealth / storageHealth | Salud. Mantener sin filtrar secretos/rutas y separar liveness/readiness. |

### Helpers restantes y adaptadores de Telegram

| Función | Responsabilidad y recomendación |
|---|---|
| mediaAPI | Obtiene cliente del data-center correcto para media. Mantener; limitar pools y cerrar clientes inactivos. |
| closeMediaPools | Cierra pools de media. Mantener; hacerlo seguro durante shutdown concurrente. |
| updateStorage.GetState / SetState | Lee y persiste pts/qts/date/seq de updates. Crítica: transacción y monotonicidad para no retroceder offsets. |
| updateStorage.setField | Actualiza un campo de estado Telegram. Mejorar whitelist estricta de nombres y una sola transacción. |
| SetPts / SetQts / SetDate / SetSeq / SetDateSeq | Persisten offsets globales. Mantener con regla monotónica. |
| GetChannelPts / SetChannelPts | Persiste offsets por canal. Mantener con upsert y monotonicidad. |
| ForEachChannels | Itera canales guardados. Mejorar cancelación de callback y cursor DB. |
| SetChannelAccessHash / GetChannelAccessHash | Cachean access hash. Mantener invalidación al cambiar peer. |
| parseMP4BoxHeader / PrepareVideoIndex / findMP4Index / readVideoBytes / warmMP4Index | Indexado y lecturas MP4. Mantener bajo demanda, con límites y sin bloquear el stream. |
| DeleteForumTopic | Borra un topic Telegram. Mantener separado de la limpieza local y con confirmación de permisos. |
| webui.Handler | Sirve frontend embebido y fallback de rutas. Mantener; no permitir path traversal ni servir archivos fuera del embed. |
| webui.FormatTime y helpers de tiempo | Formatean fechas para la interfaz. Mantener; fijar zona horaria y probar cambios de DST. |
| migrations.Files | Embed de migraciones SQL. Mantener inmutable; el checksum se calcula sobre el contenido exacto. |

Los callbacks y closures se revisaron junto con la función que los crea: deben respetar el contexto recibido, no lanzar goroutines sin ownership y devolver errores al worker que los coordina.

## Migraciones

Las migraciones 001-035 son inmutables una vez publicadas; cualquier cambio requiere una migración nueva. La 036 agrega leases sin reescribir el esquema previo. Deben probarse especialmente 006, 019-022, 028-032 y 033-036 porque afectan realtime, STRM, espejos, colas, autenticación y búsquedas.

### Compatibilidad de actualizaciones de imagen y persistencia — revisión 2026-09-26

- **Persistencia:** PostgreSQL corre fuera de la imagen TgStrm. `/data` guarda la configuración cifrada, claves, sesión Telegram y backups; los destinos STRM son montajes independientes. Reemplazar la imagen no borra esos datos si se preservan los mismos mounts y la conexión PostgreSQL.
- **Migrador:** `postgres.Migrate` usa un advisory lock dentro de una transacción, contrasta checksums, ejecuta solo migraciones ausentes y agrega el registro de cada una en la misma transacción. Rechaza una versión de esquema desconocida o más nueva y cualquier cambio de checksum no autorizado. En consecuencia, no se reejecutan migraciones ya aplicadas desde la release `v0.1.2`.
- **Migraciones revisadas:** las migraciones existentes de `v0.1.2` coinciden por nombre y contenido con las actuales; la única añadida es `036_queue_leases.sql`, que agrega campos/índices y normaliza estados de trabajos pendientes. No elimina filas del catálogo. La 008 histórica sí contenía una limpieza de datos, pero está en `v0.1.2` y queda registrada; no se dispara de nuevo durante una actualización desde esa versión. No editar una migración aplicada: crear una nueva.
- **Excepción histórica:** al actualizar desde una versión anterior a aquella que introdujo 008, esa migración sí limpia las tablas `messages` y `message_media` para volver a indexar solo vídeos. Está documentada en el SQL y no es una migración inocua para el catálogo local; no afecta a una instalación que ya la tenga registrada. Sin acceso a la DB activa no puedo confirmar qué filas del ledger tiene esa instalación.
- **Riesgo corregido:** la imagen anterior era distroless y contenía solo el binario TgStrm, aunque el flujo de backup/restore invoca `pg_dump` y `pg_restore`. Ahora la imagen incluye clientes PostgreSQL versionados 14–18, verifica su presencia durante el build y el servicio selecciona la versión principal que coincide con el servidor. En instalaciones nativas exige clientes coincidentes en `PATH`.
- **Límites de versión:** restaurar un dump generado con un PostgreSQL principal más nuevo en un servidor más antiguo queda bloqueado antes de modificar la DB. Una actualización de la versión principal del propio PostgreSQL es separada de la actualización de TgStrm y debe seguir el procedimiento PostgreSQL.
- **Operación recomendada:** copia verificada de TgStrm, respaldo privado de `/data` y copia PostgreSQL antes del upgrade; actualizar con tag de imagen concreto manteniendo montajes/env; confirmar estado saludable, migraciones y actividad. No se hizo upgrade ni restore contra la instalación activa.
- **Rollback:** el migrador rechaza arrancar un binario antiguo frente a un esquema más nuevo. Volver a la imagen previa exige restaurar también el backup PostgreSQL y `/data` tomados antes de la actualización; cambiar únicamente el tag de imagen no es rollback seguro.
- **Fuentes oficiales:** Docker documenta que volúmenes/bind mounts persisten fuera de la capa escribible del contenedor; PostgreSQL declara que `pg_dump` rechaza servidores más nuevos que su propia versión principal y que un upgrade de servidor mayor es una operación específica; PGDG distribuye clientes por versión principal. [Docker: Volumes](https://docs.docker.com/engine/storage/volumes/), [PostgreSQL: pg_dump](https://www.postgresql.org/docs/17/app-pgdump.html), [PostgreSQL: Upgrading a PostgreSQL Cluster](https://www.postgresql.org/docs/17/upgrading.html), [PostgreSQL: Apt Repository](https://wiki.postgresql.org/wiki/Apt).
- **Validación:** migraciones históricas comparadas contra `v0.1.2`; pruebas Go y Docker build pendientes de ejecutar/verificar al final. No se ha probado una subida/restauración con datos reales.

## Casos de prueba de aceptación

1. Reiniciar durante import, espejo, TMDB y generación STRM simultáneos.
2. Repetir el mismo update y editar el caption antes de terminar la copia.
3. Borrar un topic con posters/STRM y verificar ownership y carpetas compartidas.
4. Añadir un vídeo como documento, editar solo el pie y comprobar matching, reemplazo y WebSocket.
5. Hacer seek con Safari, Jellyfin y VLC con caché fría y caliente.
6. Restaurar un backup en una DB nueva y arrancar sin intervención manual.
7. Lanzar dos instancias y verificar que no duplican imports/espejos/exports.
8. Publicar tras NPM con un subdominio nuevo, WebSocket, cookies Secure y Range.

## Orden recomendado

1. Leases e idempotencia.
2. Contrato DB-first y ownership de STRM.
3. Restore verificado.
4. Backoff, límites y métricas Telegram/TMDB.
5. Auth, streaming público y proxy.
6. Estrés y matriz multiplataforma.

## Seguimiento de implementación

### 1. Leases e idempotencia — parcial, revisión 2026-09-26

- **Hecho:** la cola de importación reclama trabajos en una sola operación PostgreSQL con `FOR UPDATE SKIP LOCKED`, guarda un token de lease y lo renueva mientras ejecuta el lote. Un proceso caído deja vencer el lease y el worker puede recuperar el manifiesto; los errores liberan el lease con una transición condicional al token propietario.
- **Hecho:** el reintento manual vuelve a poner el manifiesto en la cola canónica; ya no inicia un segundo envío por fuera del worker. Se eliminó la recuperación paralela que arrancaba al conectar Telegram y podía procesar el mismo import que la cola de API.
- **Hecho:** entregas y ediciones de espejo reclaman filas atómicamente con `SKIP LOCKED`; los estados `sending`/`editing` se recuperan al vencer su lease. El historial de origen también lleva lease renovable. Las entregas de cada vínculo conservan el orden ascendente del ID de mensaje de Telegram, incluso con varios workers. Ediciones de caption que llegan durante una edición en curso quedan pendientes y se aplican después, sin perder el último texto solicitado.
- **Idempotencia externa:** los reintentos de imports y espejos conservan el mismo `random_id` para el mismo mensaje y destino. Telegram indica que ese identificador evita volver a enviar el mismo mensaje y lo usa para deduplicar solicitudes repetidas.
- **Migración:** `036_queue_leases.sql` agrega tokens, expiraciones e índices parciales; los registros que quedaron `sending`, `editing` o `indexing` con versiones anteriores quedan reclamables inmediatamente. No borra ni reescribe el manifiesto histórico.
- **Límite documentado:** escaneos TMDB y generación/reconciliación STRM aún coordinan parte del trabajo mediante locks en memoria por proceso. Eso evita duplicados dentro de una instancia, pero no demuestra seguridad con varias réplicas simultáneas. No se les añade un lease genérico en este paso: requieren definir cancelación, vencimiento y protección de escrituras por asset/ruta para no crear una falsa garantía.
- **Base oficial:** PostgreSQL recomienda `SKIP LOCKED` precisamente para varios consumidores de una tabla de tipo cola; Telegram documenta `random_id` como clave de deduplicación y asociación de envíos. [PostgreSQL: locking clause](https://www.postgresql.org/docs/current/sql-select.html#SQL-FOR-UPDATE-SHARE), [Telegram: Working with Updates](https://core.telegram.org/api/updates).
- **Validación:** `go test ./...`, `go vet ./...` y `git diff --check` pasan. La cobertura de migración 036 contra una base PostgreSQL real y el escenario de dos procesos requieren una prueba de integración posterior; por ahora la garantía se limita a las operaciones descritas arriba.

### 2. PostgreSQL como fuente canónica y STRM reparables — parcial, revisión 2026-09-26

- **Hecho:** la reconciliación compara ahora los bytes del `.strm` con el contenido reconstruible desde el `public_base_url` y el ID estable del asset guardados en PostgreSQL. Marca como error un archivo alterado y sincroniza `content_hash` cuando el contenido sí coincide.
- **Hecho:** la acción de reparación incluye archivos existentes pero corruptos y reescribe el contenido canónico; no se limita a recrear archivos ausentes.
- **Hecho:** si `RemoveAll` falla al borrar el árbol de un topic eliminado, se conservan sus filas de `stream_exports`. Así el siguiente ciclo vuelve a intentar borrar la carpeta entera, incluidos posters y otros sidecars, en vez de perder la referencia al árbol fallido.
- **Mantenido:** `atomicWrite` prepara el temporal en el directorio de destino, sincroniza el archivo y luego lo renombra. Las operaciones de borrado siguen quitando el archivo del disco antes de eliminar su fila de PostgreSQL, de modo que un fallo del filesystem no borre el rastro que permite reintentar.
- **Pendiente de la matriz final:** Go advierte que `os.Rename` no es atómico en todas las plataformas no Unix. La reconciliación detecta y corrige contenido incorrecto, pero la garantía de reemplazo sin ventana en Windows debe verificarse en la tarea de empaquetado multiplataforma; tampoco se ha ejecutado todavía una prueba de caída del proceso entre escritura, rename y commit.
- **Base oficial:** PostgreSQL confirma que sus transacciones protegen los cambios dentro de la base; no incluyen operaciones del filesystem. Go documenta que `File.Sync` vuelca el archivo a almacenamiento estable y detalla las limitaciones por plataforma de `os.Rename`. [PostgreSQL: transactions](https://www.postgresql.org/docs/current/tutorial-transactions.html), [Go `os.Rename` y `File.Sync`](https://pkg.go.dev/os#Rename).
- **Validación:** `go test ./...`, `go vet ./...` y `git diff --check` pasan. La validación de caída real y Windows queda pendiente de integración.

### 3. Restauración de backup verificable — parcial, revisión 2026-09-26

- **Hecho:** al generar un backup, TgStrm ejecuta `pg_restore --list` sobre el dump custom antes de empaquetarlo como restaurable. El flujo de restauración vuelve a revisar el listado y valida las sumas SHA-256 de todos los miembros del archivo antes de modificar PostgreSQL.
- **Hecho:** se conserva y valida una copia preventiva antes de modificar la instalación. Si PostgreSQL se restaura pero falla el reemplazo de `master.key`, `setup.key` o `database.enc`, el servicio repone tanto la base como esos archivos desde la copia preventiva; si una de las dos recuperaciones falla, responde indicando que no se debe reiniciar. Los errores encontrados al revertir renombres ya no se descartan.
- **Mantenido:** `pg_restore --single-transaction` implica salir al primer error y garantiza que la parte SQL se complete entera o no aplique cambios. El backup custom sigue siendo el formato apropiado para `pg_restore`; la extracción acepta únicamente archivos regulares con nombres allowlist y compara los hashes del manifiesto.
- **Límite importante:** PostgreSQL y tres archivos del filesystem no comparten una transacción. Una caída brusca entre el commit de la DB y el reemplazo completo del bootstrap todavía puede dejar un conjunto desparejado; el código recupera fallos que detecta en ejecución, pero no reconstruye automáticamente una operación interrumpida por apagado. No ejecuté una restauración destructiva contra la base de datos real.
- **Base oficial:** PostgreSQL describe los dumps custom como restaurables con `pg_restore`, y documenta que `--single-transaction` revierte todo ante un error; `--list` inspecciona la tabla de contenidos del archivo. Go `archive/tar` documenta los tipos de entrada del formato; el código restringe además los nombres a una lista conocida. [PostgreSQL: SQL Dump](https://www.postgresql.org/docs/18/backup-dump.html), [PostgreSQL: pg_restore](https://www.postgresql.org/docs/17/app-pgrestore.html), [Go: archive/tar](https://pkg.go.dev/archive/tar).
- **Validación:** las pruebas locales cubren suma inválida, fallo de staging sin alterar archivos y sustitución completa del conjunto de bootstrap. No se ha probado una restauración real ni la recuperación después de matar el proceso a mitad de la operación; ambos pasos requieren un entorno aislado y quedan anotados para la matriz de release.

### 4. Límites y backoff Telegram/TMDB — parcial, revisión 2026-09-26

- **Hecho:** todas las llamadas a TMDB comparten un limitador dentro de la instancia: máximo 10 solicitudes por segundo, incluyendo los scans paralelos y las peticiones interactivas. Si TMDB devuelve `429`, se congela el siguiente acceso global usando `Retry-After`; si falta o no se entiende esa cabecera, aplica una pausa mínima de un segundo y no repite automáticamente la misma llamada. Las peticiones web no quedan esperando más de 12 segundos por un cooldown largo: devuelven el error y mantienen la pausa global.
- **Hecho:** las esperas entre elementos de los scans de películas y artwork ahora observan `context.Context`, por lo que una cancelación detiene el trabajo sin esperar a que termine el `Sleep` fijo.
- **Ya existía y se conserva:** las importaciones esperan el `FLOOD_WAIT` entregado por Telegram antes de reenviar el mismo elemento; el escaneo paginado vuelve a intentar hasta cuatro veces con esa espera y cancela al cerrarse el trabajo.
- **Límite documentado:** el limitador es por proceso. Dos réplicas podrían sumar solicitudes; la coordinación persistente multiinstancia y métricas de flood-wait/429 siguen pendientes. El límite 10/s es una decisión conservadora de TgStrm, no un número garantizado por TMDB.
- **Base oficial:** TMDB desactivó su antigua cuota fija, documenta límites superiores adaptativos cercanos a 40 peticiones/s y pide respetar `429`. Telegram documenta `FLOOD_WAIT_X` como una instrucción para esperar X segundos. El paquete oficial de Go `golang.org/x/time/rate` describe el patrón de limitación token-bucket y esperas cancelables; aquí se usa un espaciador global sencillo para no añadir dependencia y para aplicar el `Retry-After` de forma centralizada. [TMDB: Rate Limiting](https://developer.themoviedb.org/docs/rate-limiting), [Telegram: API Errors](https://core.telegram.org/api/errors), [Go `rate.Limiter`](https://pkg.go.dev/golang.org/x/time/rate).
- **Validación:** hay pruebas para espaciado de llamadas, `Retry-After` y cancelación; `go test ./internal/httpapi` pasa. Aún falta instrumentar métricas operativas y probar 429/flood-wait contra respuestas simuladas del cliente HTTP y Telegram.

### 5. Seguridad HTTP, cuenta y streaming — parcial, revisión 2026-09-26

- **Hecho:** revisé cookies de sesión: token aleatorio de 256 bits, solo se almacena su SHA-256, caducidad de 24 h, revocación en DB, `HttpOnly`, `SameSite=Strict`; en HTTPS se usa nombre `__Host-`, `Secure` y `Path=/`. `X-Forwarded-Proto` solo se acepta si la conexión viene de un CIDR de proxy configurado. La protección de origen rechaza solicitudes con `Origin` ajeno y los cambios son autenticados por `adminGate`.
- **Corregido:** las solicitudes erróneas de código de recuperación no incrementaban `attempts` porque la consulta buscaba el hash exacto y terminaba antes de actualizar. Ahora se bloquea el reto más reciente, se compara el hash en tiempo constante, se cuenta cada código incorrecto y se consume al quinto intento. Las solicitudes simultáneas de recuperación se serializan por cuenta y la escritura separa las sentencias SQL para que el driver no rechace un comando múltiple parametrizado. También evité que cada solicitud durante un bloqueo de login reinicie sus 15 minutos, y añadí respuesta segura si recuperación se invoca sin DB disponible.
- **Mantenido por requisito funcional:** `/stream/{assetID}` es público porque los reproductores que abren `.strm` no llevan la cookie administrativa. El ID es UUID persistente y funciona como URL-capacidad: cualquiera que obtenga el `.strm` puede acceder; el servidor aún exige que asset, mensaje, topic y grupo sigan activos. Revocar el asset elimina ese acceso. Esto conserva los enlaces estables que requiere la biblioteca, pero la URL no debe publicarse en logs o terceros.
- **Límite documentado:** no añadí expiración al enlace público porque invalidaría `.strm` ya creados. Si la instalación se abre a Internet, la protección de acceso a los streams debe decidirse junto al uso previsto de Jellyfin/VLC; la cookie de la web no los protege. También faltan una prueba completa detrás de NPM con CIDR real, pruebas de CSRF/browser y eventos de autenticación estructurados. El usuario inicial `admin` y el botón para omitir el cambio inicial siguen siendo una decisión de usabilidad ya aprobada, con riesgo si se expone el servicio antes de cambiar credenciales.
- **Base oficial:** OWASP recomienda respuesta uniforme y rate limiting en login/recuperación, códigos aleatorios, almacenados de forma segura, de un solo uso y con caducidad. Go documenta `ConstantTimeCompare` para comparación sin depender del contenido y los campos de seguridad de `http.Cookie`. [OWASP: Authentication](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html), [OWASP: Forgot Password](https://cheatsheetseries.owasp.org/cheatsheets/Forgot_Password_Cheat_Sheet.html), [Go: crypto/subtle](https://pkg.go.dev/crypto/subtle#ConstantTimeCompare), [Go: net/http Cookie](https://pkg.go.dev/net/http#Cookie).
- **Validación:** pruebas unitarias para bloqueo temporal y fallo seguro sin DB; `go test ./...`, `go vet ./...` y `git diff --check` pasan. No hice pruebas contra el proxy real ni contra la base de datos activa.

### 6. Pruebas de concurrencia y matriz multiplataforma — parcial, revisión 2026-09-26

- **Hecho:** la CI ejecuta ahora `go test -race ./...` en Linux y un job nativo de `go test ./...` más `go build ./cmd/tgstrm` en Linux, macOS y Windows. Así los cambios se validan en las plataformas de distribución y el detector puede hallar carreras que sí se ejecutan en los tests.
- **Validado en esta máquina:** `go test -race ./...` pasa. Compilé el servicio sin CGO para `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64` y `windows/amd64`. Son comprobaciones de compilación cruzada; no prueban comportamiento real de filesystem o red en cada sistema.
- **Pendiente de entorno aislado:** los casos de aceptación que envían/editen/eliminan datos Telegram, restauran PostgreSQL, verifican el proxy real o ejecutan dos instancias no se disparan automáticamente porque podrían modificar la instalación real y requieren servicios/fixture separados. La matriz nativa de GitHub se ejecutará al enviar esta rama; desde este checkout no puedo afirmar su resultado todavía.
- **Base oficial:** Go explica que `go test -race` detecta data races solo en rutas ejercitadas y que `GOOS`/`GOARCH` permiten compilar para sistemas soportados; GitHub Actions documenta matrices para ejecutar combinaciones de runners. [Go: Data Race Detector](https://go.dev/doc/articles/race_detector), [Go: supported GOOS/GOARCH](https://go.dev/doc/install/source), [GitHub Actions: job matrix](https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/use-jobs).
- **Validación:** `go test -race ./...`, compilaciones de los cinco destinos y la suite normal pasan en macOS. `go vet ./...` y `git diff --check` se volverán a ejecutar al final. Aún faltan las pruebas de aceptación con PostgreSQL/Telegram de prueba, browser/Safari y el runner CI remoto.
