# Auditoría previa a confirmar la réplica — 2026-10-05

## Resultado de la corrección y segunda auditoría
- Duplicados: corregido. La API rechaza la confirmación con 409 `duplicate_series` antes de iniciar imports; la interfaz bloquea el botón y permite filtrar los siete topics afectados. Se mantienen las 117 selecciones.
- Rutas: corregido. Reserva y registro de ruta previa comparten transacción; la retirada requiere sucesor escrito y hashes verificados. Es persistente y recuperable tras reinicio, y `os.Root` impide escapar de la biblioteca por enlaces simbólicos. Estado escrito y firma procesada se confirman juntos.
- Identidad: añadido trabajo persistente para corregir título, marcador y presentación de los destinos replicados, según los métodos oficiales de Telegram. Revisión protegida frente a cambios manuales, presentaciones no verificadas y títulos fuera del límite; esos conflictos pasan a atención. FLOOD_WAIT conserva su espera. Trabajador independiente del reconciliador STRM.
- Estado de finalización: corregido. La interfaz distingue «Copia y espejos completados» del número de vídeos indexados, STRM escritos, errores e identidades pendientes.
- Directorios: se retiran los STRM registrados y directorios vacíos; se conservan archivos ajenos/no registrados. **No se ha implementado la migración de imágenes o archivos auxiliares no gestionados**: el servicio no dispone de prueba de propiedad de esos archivos. Una carpeta con esos contenidos puede permanecer. No se presenta como resuelto el hallazgo 4.

### Segunda auditoría
Pruebas Go completas, race, vet y build web pasan. Pruebas locales en transacciones revertidas verifican el trigger de identidad, sustitución de revisiones, conservación de rutas anteriores y rechazo de duplicados sin cambiar el plan. Pruebas de filesystem verifican hashes, idempotencia, limpieza vacía y protección contra symlinks. Comprobación visual a 390 × 844: sin desbordamiento horizontal, siete conflictos visibles y confirmación deshabilitada. API reiniciada; migración 048 aplicada tras backup local. No se confirmó la réplica ni se modificaron las asociaciones del usuario.

No se realizó una réplica real ni una corrección real de TMDB sobre Telegram: permanece pendiente la comprobación de extremo a extremo. Primero hay que resolver las coincidencias duplicadas; no se fusionan automáticamente las fuentes.

## Veredicto original de la auditoría
No se recomienda confirmar la réplica masiva hasta resolver los hallazgos siguientes. Auditoría de código, base local y pruebas; NO se confirmó el plan ni se hicieron envíos, borrados o cambios de coincidencia en Telegram. No equivale a una validación de extremo a extremo contra Telegram.

## Hallazgos

### 1. Coincidencias repetidas detienen una réplica que la interfaz permite confirmar (alta)
La selección actual tiene 117 topics elegibles incluidos, cero importaciones iniciadas y estado review. Dentro del mismo cajón se repiten estas identidades:
- TMDB 4500: Aquellos maravillosos años / Aquellos maravillosos años (1988).
- TMDB 64852: Atrapados de nuevo / Atrapados / Atrapados.
- TMDB 74430: dos topics Agatha Christie Inocencia Tragica.
`confirmReplicationPlan` verifica elegibilidad/manifiestos/asociación, pero no estas identidades duplicadas. `executeReplicationStep` calcula el nombre con título, año e ID y rechaza un título ya existente. Por tanto, podría copiar parte del plan antes de detenerse. Se debe detectar y mostrar el conflicto ANTES de confirmar; no fusionar automáticamente ni excluir selecciones del usuario. Revisar las asociaciones y decidir qué fuente conservar o cómo vincular varias fuentes a una serie.

### 2. Cambio de ruta STRM sin registro duradero de la ruta anterior (alta)
`generateTopicSTRM` en `internal/httpapi/strm_exports.go` lee la ruta previa, actualiza `stream_exports.relative_path` y después ejecuta `atomicWrite`. Si falla la escritura, el reintento lee como ruta previa la nueva; la antigua queda fuera del registro. También un reinicio entre escritura y limpieza pierde esa referencia. Hay escritura atómica del archivo individual, pero no una migración duradera de rutas. Recomendación: conservar explícitamente la ruta anterior en una tarea de cambio y retirarla solo tras confirmar escritura y estado, con recuperación al reiniciar.

### 3. Corregir TMDB no actualiza toda la presentación en Telegram (media)
`confirmTMDBMatch` guarda la coincidencia y publica `topics.snapshot`. El trigger `invalidate_tmdb_stream_assets` invalida los exports de la serie, y el reconciliador solicita regeneración. No existe en ese handler una llamada para renombrar el topic Telegram ni actualizar su marcador TMDB/presentación. El nombre antiguo con otro ID podría recuperar la asociación equivocada en una instalación desde cero. Se requiere un trabajo duradero de actualización de título/marcador/presentación si se pretende mantener Telegram y disco con la misma identidad corregida.

### 4. Directorios antiguos con imágenes pueden quedar tras cambiar coincidencia (media)
El export nuevo elimina el STRM anterior y llama a `removeEmptyExportParents`. Esa función limpia directorios vacíos, por tanto una carpeta antigua con imágenes u otros ficheros no desaparece. No es una migración completa de carpeta. Debe definirse la propiedad de los archivos y trasladar/limpiar solo lo gestionado; nunca borrar una carpeta compartida por otras entradas activas.

### 5. Finalización de réplica no garantiza todos los STRM escritos (media)
El hijo se marca completed después de importar, sincronizar catálogo y activar espejo; el plan termina cuando no quedan hijos queued. El export tiene su reconciliador independiente: permisos, identidad de episodio inválida o colisiones pueden dejar exports pendientes/error incluso con réplica completed. La interfaz debe distinguir archivos copiados, espejo activo y biblioteca STRM preparada, sin prometer la última por el estado de la primera.

## Funcionamiento verificado por inspección
- Validación final de contenido completo antes de crear destino; documentos de vídeo incluidos; un topic con documentos incompatibles queda excluido completo.
- Coincidencia y metadatos se persisten en destino mediante `persistCatalogDestination` antes de continuar la importación; evento de snapshot publicado.
- Orden de importación por temporada/episodio reconocido, con fallback por ID Telegram (`sortImportDocuments`).
- Espejo automático mediante `ActivateReplicationMirror`, después de sincronizar destino y verificar identidades; reutiliza entregas de importación y encola cambios posteriores al manifiesto sin duplicarlas.
- Trabajos e identidades persistentes, leases, espera FLOOD_WAIT y recuperación. El navegador no es el propietario del trabajo.
- Recepción de nuevos mensajes/ediciones/borrados por dispatcher; estado de secuencias persistido con gotd updates y recuperación de huecos.
- Ediciones externas del pie se encolan; un cambio manual del pie destino provoca conflicto y no se sobrescribe silenciosamente.
- Borrar en origen no implica borrar la copia destino; borrar en destino marca tombstone para evitar recrearla.
- WebSocket publica snapshots/progreso; revisión mantiene escucha y sondeo de recuperación de 15 s. El disco se reconcilia periódicamente (2 s): no todos los cambios son instantáneos ni exclusivamente WebSocket.
- Nuevos cajones de colección están incluidos en `managed_catalog_chats`; comparten generación automática con grupo principal.

## Pruebas realizadas
- `go test ./...`: pasa.
- `go test -race ./internal/httpapi ./internal/telegram ./internal/library`: pasa.
- `TGSTRM_TEST_DATA_DIR=... go test ./internal/httpapi -run TestCollectionReadModels -count=1 -v`: pasa (collections, target, jobs).
- Consulta de duplicados y estado del plan: solo lectura.
- TestCabinetPhotoQueueSQL omitido por su variable opt-in específica; no afirmar que se ejecutó.
- No prueba real de réplica masiva ni de cambio de coincidencia sobre archivos reales. Las pruebas existentes no cubren la caída durante migración de rutas ni la serie duplicada en una selección.

## Fuentes oficiales
- Telegram updates y recuperación de secuencias: https://core.telegram.org/api/updates
- Identidades aleatorias en envíos: https://core.telegram.org/method/messages.forwardMessages (la implementación usa sendMedia y random_id estable).
- TMDB detalles de serie: https://developer.themoviedb.org/reference/tv-series-details

## Antes de confirmar
1. Resolver identidades repetidas en la revisión y añadir validación previa visible.
2. Hacer duradero el cambio de rutas y definir la actualización de topic/presentación al corregir TMDB.
3. Verificar con un topic controlado: copiar, espejo, corregir asociación, añadir/editar episodio y reiniciar durante un trabajo. Revisar Telegram, base, disco e interfaz por separado.

## Fuentes de la corrección
- Editar título y límite UTF-8: https://core.telegram.org/method/messages.editForumTopic
- Editar presentación en su mensaje original: https://core.telegram.org/method/messages.editMessage
- UPSERT y transacciones de reserva: https://www.postgresql.org/docs/current/sql-insert.html

## Corrección de revalidación — 2026-10-06
Se detectó que todos los manifiestos habían sido invalidados por eventos mientras el plan seguía `review`, sin `confirmed_at`; no había imports ni destino creado. El reinicio de preparación antes solo consideraba planes confirmados. Ahora los borradores en revisión también recuperan esa preparación, y el mantenimiento repara estados ya invalidados. La vista lee `pending` cuando evidencia y revisión no coinciden, impidiendo ofrecer confirmación falsa. Revalidar conserva `excluded` y las coincidencias ya guardadas y no repite las búsquedas TMDB de esas asociaciones. Un cambio durante el escaneo reintenta preparación en lugar de dejarlo bloqueado en atención. Los errores de acción están separados de errores de sondeo y ya no desaparecen al refrescar datos.

Verificación: tests Go y race pasan; frontend compila. Tras reiniciar, el plan pasó a `preparing`, conserva 114 incluidos y 3 excluidos manualmente, sin confirmación ni imports; se observó avance de manifiestos comprobados. Esta comprobación es lectura de Telegram, no una réplica real. Hay que esperar a que termine la revalidación antes de confirmar.

### Optimización del análisis y espera visible
La preparación leía cada topic con SyncTopicVideoCatalog (historial + dos filtros de búsqueda) y después con ScanPersistedTopicEligibility. Se eliminó esa sincronización previa redundante: las páginas completas de getReplies de la validación se indexan mediante el mismo storeVideoMessage transaccional. Se mantienen clasificación de vídeos-documentos, detección de incompatibles, paginación hasta completar y la barrera de revisión ante cambios concurrentes. No se aumentó paralelismo ni se omitió FLOOD_WAIT. Las rutas habituales de sincronización fuera de esta preparación conservan sus tres vías.
El éxito del paso elimina last_error obsoleto y no añade una espera artificial de un segundo. El contador real se muestra junto al título, sin una fila adicional. Tests Go, race, vet y build pasan; la API se reinició y la vista mostró avance a 84/126 comprobados. No se midió todavía una reducción de tiempo total comparable: se verificó la eliminación de recorridos RPC redundantes, no una velocidad garantizada.
Fuentes: https://core.telegram.org/method/messages.getReplies y https://core.telegram.org/api/errors.

### Recuperación de revisión discordante
Se encontraron siete manifiestos `scan_failed` con revisión distinta de la enlazada al topic, sin `invalidated_at`. La selección de preparación solo buscaba invalidación explícita o estado del hijo, por lo que esos registros no se retomaban. Preparación y reparación de borradores consideran ahora también la discordancia de revisiones; los excluidos no se reescanean. La interfaz explica la condición que deshabilita confirmar. Tests de paquetes y build pasan; tras reiniciar se observó reducción de siete a dos comprobaciones pendientes, cero imports. Se mantienen las decisiones del usuario.

## Corrección de pies importados — 2026-10-06

La activación del espejo insertaba el pie normalizado como `last_applied_caption`,
pero omitía `source_edited_at`. La reconciliación interpretaba la diferencia
intencionada entre origen y destino como una edición no procesada y volvía a
aplicar el pie original, incluso vacío. Ahora la entrega importada conserva
también la revisión temporal del mensaje origen. Los cambios posteriores siguen
siendo elegibles para la sincronización existente.

Verificación: pruebas de `internal/telegram` e `internal/httpapi` satisfactorias.
La réplica real figura completada con 114 imports y 3.695 archivos.
Se encoló la recuperación de los pies históricos de este plan mediante la cola
de edición existente, sin reenviar vídeos ni recrear topics. Se preservaron
conflictos manuales y destinos eliminados; cada edición verifica el texto remoto
contra el último aplicado antes de modificarlo. La recuperación es persistente
y todavía está en ejecución: se han observado ediciones confirmadas y
FLOOD_WAIT de Telegram. La indexación local puede tardar después del acuse de
edición, por lo que no se declara recuperación completa hasta comprobar la cola.
Referencia oficial: https://core.telegram.org/method/messages.editMessage

## Segunda auditoría y recuperación definitiva del flujo — 2026-10-06

La corrección basada únicamente en fecha no bastó. Tanto la recepción de
ediciones como la reconciliación deben distinguir el texto fuente del texto
normalizado destino. La migración 049 añade `source_caption_snapshot`; activación
de réplica, envío, adopción de entregas, edición realtime y recuperación conservan
esa referencia. Los eventos con texto idéntico no encolan otra edición; los
cambios reales sí actualizan la referencia y la cola de forma atómica.

La prueba con PostgreSQL encontró además una referencia no válida al alias
objetivo `md` dentro de un JOIN de la consulta realtime. Se movió esa relación
al WHERE. La prueba ejecuta los SQL de producción y se revierte íntegramente:
origen vacío/destino normalizado, evento repetido, edición real, repetición
durante edición pendiente y recuperación de una edición offline.

El editor ahora detiene la tanda ante FLOOD_WAIT y aplaza las ediciones
pendientes de la cuenta durante al menos el tiempo indicado, en vez de probar
otros mensajes inmediatamente. Se conserva la comprobación del texto remoto
antes de sobrescribir un destino editado manualmente.

Restauración real: se encolaron los pies originales normalizados de todos los
topics replicados activos con entrega identificada. Para 34 archivos cuyo
episodio no se pudo identificar se preparó título y año confirmados, sin
inventar numeración. La importación futura aplica ese mismo fallback si el pie
estaba vacío. Esos archivos continúan necesitando revisión para identificar el
episodio y no se declaran capítulos identificados.

Validación: go test ./..., go test -race para telegram/httpapi con PostgreSQL
opt-in y go vet de ambos paquetes pasan. Fuente oficial consultada:
https://core.telegram.org/api/updates
https://core.telegram.org/method/messages.editMessage
https://core.telegram.org/api/errors

Estado de la operación externa al cerrar esta auditoría: no se declara
terminada. 2.029/3.701 pies confirmados con el texto preparado; Telegram impuso
FLOOD_WAIT de 1.763 segundos y la siguiente oportunidad de reintento quedó
guardada aproximadamente a las 13:59:29 Europe/Madrid. Se comprobó que no había
referencias fuente divergentes, conflictos manuales ni tareas sin pie deseado.
El servicio queda funcionando para continuar la cola. Debe verificarse al
finalizar que todas las ediciones están confirmadas y permanecen estables.

## Corrección completa de una serie replicada — 2026-10-06

Antes, la corrección TMDB encolaba título y presentación, pero no actualizaba todos los pies ni invalidaba asignaciones manuales pertenecientes a la serie anterior. Ahora usa el mismo diálogo de Gestión y un trabajo persistente con revisiones y fases: identidad/presentación, preparación de pies, edición confirmada en Telegram, generación de biblioteca y retirada de rutas antiguas verificadas.

- Migraciones 050/051 amplían el trabajo sin sustituir tablas ni reiniciar datos. La primera confirmación después de perder una coincidencia también encola la corrección.
- Se editan los mensajes existentes mediante messages.editForumTopic/messages.editMessage. No se reenvían vídeos; los identificadores de mensajes, activos de streaming y espejos se conservan.
- La preparación toma números del nombre o del pie original. Solo infiere una temporada ausente cuando TMDB confirma exactamente una temporada regular y el episodio existe en ella. Lo ambiguo queda para asignación manual; esa asignación reanuda la actualización del pie y STRM.
- Se descartan las asignaciones manuales de otra identidad TMDB. El editor de espejos comparte el bloqueo por topic con la corrección para impedir escrituras simultáneas. Los eventos repetidos del origen no revierten el pie normalizado.
- Las fases quedan guardadas y respetan FLOOD_WAIT. El frontend recupera el estado al abrir la vista y se actualiza mediante los eventos existentes.
- La biblioteca reutiliza el generador y la retirada segura existentes: elimina un STRM antiguo registrado solo después de verificar su sustituto y hash, y elimina los directorios que queden vacíos. No elimina archivos ajenos o modificados manualmente.
- Validación: tests Go, race detector en httpapi/telegram con pruebas PostgreSQL en transacciones revertidas, go vet y compilación TypeScript/Vite. Auditoría visual del diálogo a 390 px y escritorio.
- Pruebas de regresión: cambio de TMDB invalida override antiguo y firma de exportación; INSERT de una coincidencia en un destino replicado encola el trabajo; códigos de episodios/rangos y nombres sin temporada; preservación de captions frente a replay del espejo; retirada de STRM con hash y confinamiento de rutas.
- Limitación de la comprobación: no se ha elegido una nueva identidad ni ejecutado una corrección real sobre Animal porque el TMDB correcto todavía no está confirmado por el usuario. No se declara probada la edición remota completa de ese topic.

Referencias oficiales: https://core.telegram.org/method/messages.editForumTopic ; https://core.telegram.org/method/messages.editMessage ; https://core.telegram.org/api/errors ; https://developer.themoviedb.org/reference/tv-series-details ; https://developer.themoviedb.org/reference/tv-episode-details ; https://jellyfin.org/docs/general/server/media/shows/

### Detección complementaria de episodios — 2026-10-06

La detección común ahora prioriza un código completo del pie frente al nombre. Si el pie no contiene uno, conserva la lectura del archivo y los formatos existentes (SxxExx, NxNN, capítulos compactos y rangos). Solo después de revisar ambos se acepta una etiqueta explícita Episodio/Episode/Capítulo N sin temporada; según la regla solicitada se usa temporada 1, o una temporada explícita separada en el pie/nombre. Años, resolución y episodio cero no se interpretan como episodios. Esta suposición de temporada 1 es una convención del producto solicitada por el usuario, no una garantía de Telegram/TMDB.

Se añadieron regresiones para pie sin nombre, nombre sin pie, conflicto de códigos completos, temporada separada en la otra fuente y números no válidos. Go test ./..., race detector httpapi/telegram y go vet pasan. No se modificó el frontend ni las rutas/enlaces de streaming. Se reencoló únicamente la fase de pies de Animal conservando su identidad TMDB y el resto de colas. Primera comprobación real: 9 STRM escritos y 3 de 9 pies nuevos confirmados (resto pendiente en ese momento).

El formato exportado mantiene Season 01 y S01E01 conforme a https://jellyfin.org/docs/general/server/media/shows/ .

### Numeración simple de Undergrads — 2026-10-06

Se verificaron los 13 nombres reales: `01 Fiesta …avi`, `03_Virgenes…avi` y similares. La detección común añade, como último recurso después de los códigos completos y etiquetas Episodio/Capítulo, un número inicial de exactamente dos cifras separado del título (o el número solo). Funciona tanto en el pie como en el archivo. Conserva temporada explícita en cualquiera de los dos; en su ausencia usa temporada 1 según la convención solicitada. No toma años, resoluciones, números dentro del título o fechas numéricas como episodios. Se conservan los IDs, archivos de Telegram, estructura de salida y interfaz. Tests de regresión y race detector pasan; go vet pasa. Se encolaron exclusivamente los pies y STRM de Undergrads sin volver a renombrar el topic ni reenviar vídeos.

### Códigos compactos sin etiqueta (101/102) — 2026-10-06

Alias Charlie Sheen contiene dos archivos `_101.mkv` y `_102.mkv`. Se añadió un resolutor contextual complementario, utilizado por preparación de pies, importación, exportación, huecos y sugerencia de asignación. Después de los formatos anteriores, un token numérico aislado de tres cifras produce candidatos temporada=n/100 episodio=n%100, temporada=n/10 episodio=n%10 y temporada1 episodio=n. Consulta las temporadas del TMDB asociado y sus episodios reales: acepta exactamente un candidato; no decide por el número de archivos disponibles, pues pueden faltar capítulos. Si TMDB falla o varias interpretaciones son válidas, conserva revisión y no inventa una asignación. Solo toma un token aislado y rechaza años, resoluciones y múltiples códigos sin etiqueta. Usa los endpoints oficiales de detalles de serie/temporada. Una vez normalizado, SxxExx evita repetir la inferencia contextual.

Pruebas: S01E01, S10E01, episodio101, ambigüedad, inexistencia, números en pie sin nombre, años/resolución/múltiples tokens. Race detector httpapi/telegram y go vet pasan. Se reencoló solo la normalización/exportación de Alias Charlie Sheen, conservando Telegram y enlaces.
Referencias: https://developer.themoviedb.org/reference/tv-season-details ; https://jellyfin.org/docs/general/server/media/shows/

### Episodios N de M — 2026-10-06

Detección contextual adicional para `01_de_03`, `02 de 03` y `03_of_03`, tanto en archivo como en pie. Respeta códigos S/E existentes antes de inferir. Rechaza episodio cero, total cero y episodio superior al total. Los formatos simples no consumen el primer número de una fracción sin validar contexto. Se comparan temporadas reales con episode_count igual al total y episodios devueltos por TMDB; solo una combinación válida se acepta, respetando la temporada explícita si existe. No convierte el total en temporada ni decide por archivos locales que podrían estar incompletos. Las reglas anteriores permanecen disponibles. Tests, race detector y go vet pasan. Reprocesamiento limitado a los tres vídeos de Apocalipsis: Stalin, conservando identidad y enlaces.

### Alias el Mexicano: resultados anteriores de detección — 2026-10-06

Se revisaron los 78 pies reales sin nombre de archivo (excepto dos). La detección actual reconoce Capitulo 1, Capitulo5 y Capítulo 78 final. Se añadió una regresión que verifica todas las variantes para capítulos 1..78; race detector pasa. La causa era el estado procesado/error anterior: el reconciliador de Telegram actualiza processed_at al releer sin cambios, pero el exportador no reintenta una firma ya procesada. No se atribuye esa fecha a una nueva detección fallida. Se invalidaron exclusivamente las 78 firmas con ese error del topic y se encoló su fase de pies manteniendo la coincidencia actual. Comprobación real: 78 STRM escritos y 4 pies nuevos confirmados; 74 ediciones aún pendientes en ese momento. No se reenviaron ni borraron vídeos, ni se cambió el parser o interfaz para solucionar este caso.

### Recuperación automática de resultados antiguos — 2026-10-06

Migración 052 añade un registro de reevaluación por mensaje, separado de processed_at (que también modifica la reconciliación Telegram). El reconciliador existente comprueba hasta 20 errores de identificación por ciclo, solo en destinos replicados activos y gestionados, con TMDB asociado, respetando pausa de limpieza y trabajos de corrección pendientes/atención. Revisa el nombre/pie ya guardados: no solicita historial Telegram. Reutiliza el resolutor contextual cuando hace falta TMDB. Si ahora reconoce el episodio, encola la fase de pies/biblioteca del trabajo existente conservando título, presentación, IDs y espejos. No reinicia trabajos pendientes o en atención.

Un error aún ambiguo no se vuelve a examinar hasta un día después, salvo cambio de firma o de versión de detección. La constante episodeDetectorVersion debe incrementarse cuando se modifique la semántica del detector; los despliegues repetidos de la misma versión no reintentan todo. El registro persiste tras reinicio. La comprobación diaria es una decisión de ingeniería del producto, no un intervalo impuesto por Telegram/TMDB. Las escrituras remotas siguen usando la cola previa y respetando FLOOD_WAIT.

Auditoría: tests PostgreSQL en transacción revertida prueban caso nunca evaluado, ausencia de repetición durante el plazo, nueva versión, plazo vencido y firma distinta. Race detector httpapi/telegram pasa. Comprobación real: se crearon 7 registros de reevaluación y se encoló una recuperación automática (topic 4100462); los no resolubles quedaron diferidos al día siguiente. No se cambió la interfaz ni se relee Telegram para realizar la detección.

### Diálogo de asignación manual: confirmar recortado — 2026-10-06

La lista extensa de episodios y los demás bloques compartían una cuadrícula con altura máxima y overflow hidden. El pie podía quedar recortado. Se limita el cambio al JSX/CSS del diálogo: cabecera y pie sin contracción, cuerpo independiente con min-height 0 y scroll, y una sola zona desplazable. Verificación real con 26 episodios: seleccionar E01 habilita Confirmar y generar, visible tanto a 390x844 como en escritorio. No se confirmó ninguna asignación ni se alteraron archivos durante la prueba. Compilación TypeScript/Vite pasa.

### Confirmación manual: consulta SQL y estado visual — 2026-10-06

La asignación se revertía por comparar md.destination_message_id::text con el parámetro $2 ya tipado bigint en el INSERT de correcciones de pies. Se reprodujo con PREPARE dentro de una transacción sin cambios persistentes (operator does not exist: text = bigint). Se corrigió a comparación bigint=$2::bigint y se extrajo la consulta a manualEpisodeCaptionSQL para probar exactamente el SQL productivo en una transacción revertida. La prueba comprueba desired_caption y status pending; pruebas completas Go, race detector y go vet pasan.

Prueba real desde el navegador: archivo Las_Aventuras_De_Superman_1952_T1_01_Superman_En_La_Tierra.mp4 (mensaje 6584462) asignado a S01E01; la ventana cerró con resultado STRM creado. PostgreSQL confirma override temporada1 episodio1 y stream_exports written con ruta Season 01. El frontend deja de mostrar pending tras una respuesta de generación correcta y actualiza números/retira el hueco correspondiente inmediatamente. No se elimina ni reenvía el vídeo. El pie remoto se tramita con el trabajo persistente existente cuando el topic lo tiene.

### Normalización tras asignación manual — 2026-10-06
- Corregido el caso sin trabajo de identidad: la misma transacción crea el trabajo de edición de pies, sin renombrar el topic ni cambiar su presentación. Se conserva la revisión de trabajos existentes.
- Prueba SQL con trabajo previo eliminado dentro de una transacción revertida: pasa. Superman S01E01 confirmado en Telegram y base de datos: `Las aventuras de Superman (1952) S01E01 Episodio 1`; corrección y trabajo completados.
- Orden de importación complementado con nombre de archivo cuando el pie no contiene SxxExx, incluyendo T1_01. Prueba de episodio 1 con ID posterior al episodio 2. No se reenvían archivos existentes.

### Formato abreviado Cap — 2026-10-06
- El detector de episodios sin temporada admite ahora `Cap 1`, `Cap. 03` y `Cap_04` tanto en nombre como en pie. Mantiene prioridad de códigos completos y temporada explícita; sin temporada aplica la convención existente de temporada 1.
- Pruebas de ambos orígenes, código completo prioritario, temporada en la otra fuente y falsos positivos (Capacidad/Recap/episodio cero). Pruebas HTTP/Telegram con race y PostgreSQL local, y go vet: correctas.
- Versión del detector incrementada para permitir revisión automática de errores antiguos bajo la cola y límites existentes, sin reenviar los archivos.

### Aída: formatos T/C y numeración acumulada — 2026-10-06
- Admitidos T9 C01, T10 C46 y T11 C38 tanto en nombre como en pie. Pruebas conservan los formatos previos.
- Para nombres 5x56 / 6s89 cuyo número supera el rango oficial de esa temporada, se consulta la temporada completa de TMDB y se exige un único título idéntico tras normalizar puntuación, mayúsculas y acentos. No se resta un desplazamiento ni se asigna por posición. La metainformación de temporada se reutiliza desde PostgreSQL.
- Auditoría de los 280 vídeos de Aída contra TMDB: 9 cambios de episodio seguros y 14 títulos ambiguos/especiales. Los C20 repetidos no se reinterpretan como C21. No se eliminaron vídeos.
- La retirada destructiva de STRM fue rechazada por revisión automática; se sustituyó por archivo reversible de contenido y registro, verificado con hash y os.Root, en .tgstrm-recovery/episode-corrections de la biblioteca. La limpieza habitual excluye esas copias. La regeneración durante corrección de identidad no puede disparar sustituciones de vídeos de Telegram.
- Cola real preparada: 125 pies ya correctos se omiten y 155 requieren edición; 24 STRM existentes archivados antes de regenerar. Trabajo persistente pending sin error al verificar; no se declara terminado mientras continúan las ediciones y los límites de Telegram.
- Pruebas de regresión HTTP/Telegram con race y PostgreSQL local, go test ./... y go vet: correctas.
- Fuente oficial para episodios/nombres por temporada: https://developer.themoviedb.org/reference/tv-season-details. El análisis de nombres es lógica propia, no una regla de TMDB.

### Finalización y auditoría posterior de Aída — 2026-10-06
- Corregida la comprobación final para excluir .tgstrm-recovery también al determinar si quedan retiradas pendientes; antes el trabajo regeneraba repetidamente porque la limpieza conservaba esas mismas copias.
- Nueva prueba PostgreSQL con rollback ejecuta la consulta real: una copia recuperable no bloquea, una ruta obsoleta ordinaria sí. Pruebas HTTP/Telegram con race y go vet correctas.
- Auditoría repetida de los 280 archivos contra el detector contextual y los metadatos de temporadas TMDB: 0 cambios seguros pendientes, 14 identificaciones no concluyentes. Comprobación de las 280 correcciones: todas completed y body=desired_caption, después del reconocimiento remoto de la edición por el worker.
- Resultado real: trabajo completed sin last_error; 263 vídeos resueltos con STRM activo y 17 para revisión. No se fuerza la identificación de los 14 dudosos ni se borran los 3 vídeos que coinciden con episodios ya representados.
- Tres colisiones: 6x86 y 6x87 comparten el título Hasta que llegó Soraya; dos T9 C20 y dos T10 C20. Los nombres no prueban que el contenido sea idéntico. La corrección no permite borrar el antiguo por una coincidencia durante remapeo.
- Ejemplos no concluyentes: Fuera de caja/Vuelta de tuerca, Lo peor de mí/Lo mejor de mí, especial Los mejores momentos, Mama que lo que quiere el negro y títulos recortados. Necesitan comprobación del usuario y asignación manual; no se usaron títulos semejantes como prueba suficiente.
- El filtro Por revisar del topic permite localizar los 17 casos. Asignar episodio guarda el override, encola el pie normalizado y genera el STRM con la misma lógica corregida. Confirmar otro episodio para un archivo dudoso requiere comprobar su contenido; nunca se decide por posición en el chat.
