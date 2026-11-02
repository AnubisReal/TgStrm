# Carpeta principal: arquitectura y tareas

Estado: diseño y reglas iniciales implementados. La función completa todavía NO está disponible. No se han creado carpetas, grupos ni reenvíos en Telegram, ni cambiado el grupo principal guardado.

Fase actual: implementación por tareas autorizada, con auditoría antes de integrar cada entrega. Las nuevas reglas y la migración están en desarrollo; el modo carpeta no está activado en la instalación actual.

## Contrato de producto

Dos destinos compatibles: grupo principal (actual) o carpeta principal (nuevo). Cambiar de modo no borra el destino anterior ni sus vínculos ni sus STRM. No ejecutar automatismos simultáneos sobre dos destinos para el mismo envío. La carpeta agrupa grupos propios, con sus topics; no es una carpeta de archivos STRM. Las rutas físicas configuradas para películas y series siguen separadas.

La colección debe existir en TgStrm con UUID estable y podrá estar asociada a una carpeta REAL de Telegram. Crear la carpeta no implica crear todos los grupos ni replicar contenido. Telegram no tiene carpetas anidadas: los cajones son grupos de la carpeta, los topics pertenecen a esos grupos. Si la cuenta agota el límite de carpetas/chats, mostrar el bloqueo y conservar el trabajo; no fingir que la carpeta real se creó.

Ejemplo: carpeta «Mi biblioteca» → grupos «Series A», «Series H», «Series N», «Películas». Sus títulos son personalizables. Para otra fuente de la misma letra: sugerir otro cajón «Series N 2» o elegir el existente y revisar colisiones; nunca fusionar automáticamente por nombre. La letra es una clasificación explícita, no parte de la identidad y no cambia al renombrar un grupo.

Cobertura: A–Z, Ñ, 0–9 y Otros; A–Z/Ñ es el conjunto inicial de letras esperadas y debe poder ajustarse. «Sin vincular» significa que falta una vinculación en NUESTRA colección; no prueba que exista ese grupo en el origen. Separar: sin vincular, configurado, importando, al día, pausado y requiere atención. Para números y Otros, mostrarlos como secciones opcionales. Una letra vinculada no significa que todos sus archivos se hayan copiado.

## Vistas y flujo

- Ajustes, bloque actual «Grupo principal»: selector de modo Grupo/Carpeta; conservar selección de grupo. Modo Carpeta permite elegir carpeta existente o crearla con nombre y previsualización; indicar permisos y límites antes de enviar cambios. No cambiar el modo activo hasta completar y persistir la configuración.
- Biblioteca normal: mostrar la carpeta activa y sus cajones, cobertura compacta y filtro por letra/tipo. Entrar al cajón muestra topics como ahora; entrar al topic conserva archivos, temporadas, huecos, TMDB y estados STRM.
- Inicio normal: reutilizar la lista y búsqueda de grupos actuales para encontrar los cajones externos por nombre; por ejemplo, buscar «cajón de series». Esto es búsqueda de chats accesibles a nuestra cuenta, no el buscador global de archivos ni una búsqueda pública de grupos de todo Telegram.
- Inicio / Gestión lateral: mostrar selección individual, deseleccionar y «Seleccionar todos los resultados» junto con «Replicar en biblioteca». No convertir el acto de seleccionar en un envío. Resolver todos los resultados del filtro, incluso paginados, y guardar los IDs seleccionados como selección estable; indicar su cantidad y no añadir grupos nuevos que aparezcan después de confirmar la selección. Si cambia la búsqueda, conservar los seleccionados con resumen explícito y opción de limpiar. No mezclar grupos propios de destino con grupos externos candidatos.
- Revisión de replicación desde Inicio: elegir carpeta destino, comprobar letras/tipo, nombres y destinos propios nuevos/existentes, revisar TODOS los topics y sus exclusiones, coincidencias TMDB, conflictos y permisos. Reutilizar la revisión TMDB existente con agrupación por cajón. Confirmación inicia un trabajo durable, no espera la copia en la petición HTTP. Si falta carpeta, ofrecer acceso a Ajustes para crearla sin perder el borrador.
- Biblioteca / Gestión lateral: administrar únicamente nuestros cajones y sus contenidos (estados, exclusiones, pausas y acceso a espejos); la entrada principal para AÑADIR cajones externos está en Inicio / Gestión. No duplicar otro selector de fuentes en Biblioteca.
- Espejo: conservar estética actual. Añadir filtro «Requieren atención» con motivos: origen eliminado, acceso perdido, destino eliminado, revisión de identidad. Mostrar por separado grupos y topics; desplegar detalles solo cuando haga falta.
- Actividad: progreso agrupado por importación → cajón → topic; avances por WebSocket, errores destacados, pausar/reanudar/reintentar. No una notificación por archivo.
- Móvil: acciones secundarias por icono etiquetado, diálogo navegable con buscador fijo, sin tablas anchas ni scroll horizontal; reutilizar componentes existentes.

Rutas propuestas: /home para Inicio y su modo de gestión; /imports/replications/:planID/review para un borrador de revisión persistido (recargable, sin iniciar envíos); /library, /library/collections/:collectionID, /library/collections/:collectionID/manage, /library/collections/:collectionID/groups/:groupID, /library/collections/:collectionID/groups/:groupID/topics/:topicID. El plan ID representa una operación durable con selección guardada, no un hash que reconstruya IDs. Mantener intactas las rutas actuales de revisión individual por IDs; este borrador de operaciones masivas no las reemplaza. Validar pertenencia en servidor. Mantener rutas /groups y /library/topics existentes y enlaces STRM estables; ninguna URL multimedia debe depender del nombre/carpeta nueva. Cambiar modo normal/gestión mantiene el comportamiento de historial ya implementado: no crear un paso extra de Atrás solo por cambiar la selección lateral.

## Formato de nombres de cajones

En Ajustes, dentro de la configuración de carpeta principal, permitir un formato separado para cajones de series y películas. Por defecto: `Series {letra}` para series y `Películas` para un cajón de películas sin clasificación alfabética; si se organiza por letras, `Películas {letra}`. Variables admitidas inicialmente: `{letra}` y `{parte}`; sin expresiones ni ejecución de código. La parte 1 no añade sufijo automático; desde la parte 2, añadir ` 2`, ` 3`, etc., salvo que la plantilla ya incluya `{parte}`. Mostrar ejemplos reales del resultado antes de guardar. Validar variables, títulos vacíos, longitud admitida por Telegram y ambigüedades con los cajones existentes.

Guardar el formato afecta solo a cajones nuevos. Para los existentes habrá un botón separado «Aplicar a los cajones existentes»: previsualización de nombre actual → propuesto, selección individual/todos y confirmación. Solo se renombrarán grupos propios asociados a esa colección y para los que la cuenta tenga permisos; nunca los grupos de origen, los topics, la carpeta principal ni las carpetas físicas/STRM.

Conservar por cajón el modo de nombre (automático/manual), la revisión de plantilla aplicada y el último nombre aplicado. Los nombres manuales se excluyen por defecto, pero pueden seleccionarse expresamente. Si alguien renombró el grupo en Telegram desde la última aplicación, mostrar el conflicto y excluirlo hasta revisión; no sobrescribirlo silenciosamente. Distinguir el nombre visto actualmente del nombre deseado y no inventar que Telegram ya aceptó un cambio.

Aplicación masiva como trabajo durable: guardar cada operación, respetar FLOOD_WAIT, comprobar el título real ante respuestas ambiguas, informar éxito/error por cajón y permitir reintentar solo pendientes. Una nueva edición de formato no altera un trabajo ya confirmado: usar su revisión guardada. Revalidar permisos y nombres antes de ejecutar. Los títulos duplicados no son identidad ni ocasionan fusiones. Los updates de título refrescan la interfaz por el canal de tiempo real existente. Los IDs, vínculos, letra asignada, episodios, matcheo y enlaces STRM permanecen intactos.

## Identidades, desconexión y recuperación

Guardar cuenta + ID Telegram de grupo + ID Telegram de topic; IDs locales solo como referencia adicional. El nombre es presentación. Mismo ID con otro nombre: actualizar metadatos y conservar espejo, archivos y letra. Si un topic cambia de serie/TMDB, pedir revisión antes de cambiar STRM existentes.

Si el origen desaparece: detener nuevas copias de ese vínculo, conservar destino/STRM y registro de procedencia. Si se pierde acceso o red: marcar acceso perdido/desconocido, NO eliminado. Un escaneo incompleto no prueba una eliminación. Si recrean un topic con ID diferente: sugerir candidatos por TMDB+tipo, año y título; confirmar sustitución y conservar trazabilidad del vínculo antiguo. Ninguna reconexión basada solo en «N» o títulos similares.

Destino eliminado por el usuario: aplicar comportamiento actual de limpieza del destino, detener espejo y no recrearlo automáticamente. Eliminar una carpeta Telegram o quitar un grupo de ella no equivale a borrar sus chats: conservar contenido y solicitar reparar asociación. Renombrar carpeta o grupo no borra rutas físicas.

Nuevos topics en un cajón ya vinculado: descubrirlos por updates y recuperación al reconectar. Matcheo seguro crea destino; coincidencia ambigua queda en revisión. El usuario puede excluir topics. General oculto no se importa. Vídeos enviados como vídeo o documento siguen el detector común; no textos normales.

### Elegibilidad obligatoria antes de crear un topic destino

Regla acordada: la elegibilidad se decide para el TOPIC COMPLETO, no filtrando capítulos individualmente. Debe contener al menos un vídeo admitido por el detector común (vídeo Telegram o documento de vídeo) y no contener archivos de capítulos en formatos no vídeo. Si cualquier capítulo está en ZIP, RAR, comprimido dividido u otro formato que no sea vídeo, NO se duplica ese topic: no se crea destino, no se envía el póster y no se copian sus otros vídeos. Ejemplo obligatorio: 10 vídeos + 1 capítulo en ZIP = topic completo excluido, NO copia parcial de los 10 vídeos. Un documento no equivale a un vídeo; no extraer comprimidos ni considerar vídeos que hipotéticamente estén dentro. Las imágenes, el mensaje de presentación y los textos no son archivos de capítulos y no excluyen el topic. Si un documento no vídeo tiene clasificación dudosa, dejarlo en revisión antes de autorizar la copia; no asumir silenciosamente que es auxiliar. Un vídeo sin episodio/matcheo identificable sigue calificando como vídeo aunque el STRM espere revisión.

El conteo local 0 no demuestra ausencia: indexar con paginación completa y guardar cursor/estado. Separar pendiente de comprobar, comprobado sin vídeos, excluido por capítulos no vídeo, requiere revisión, elegible y error de comprobación. No excluir un topic por páginas iniciales vacías, acceso perdido o escaneo interrumpido. Es obligatorio terminar la comprobación de TODO el topic antes de crear destino, enviar póster o empezar a copiar: encontrar un vídeo en la primera página no basta, ya que una página posterior puede contener un capítulo comprimido. El manifiesto y la revisión deben incluir los documentos no vídeo para evaluar esta exclusión, aunque el catálogo actual de vídeos no los muestre. Reutilizar la paginación de historial/hilos y añadir clasificación de documentos al escaneo, sin cambiar la vista del catálogo para mostrar ZIP/RAR. No basta buscar solo con filtro vídeo. Revalidar cambios recibidos durante la revisión antes de confirmar el plan. No crear grupos destino vacíos solo porque sus topics todavía no se comprobaron: diferir creación del cajón hasta tener al menos un topic elegible, conservando la asociación propuesta y su estado.

Telegram no proporciona una transacción que congele el historial mientras se copia: guardar el límite superior del manifiesto, revisión y cambios conocidos; comprobar modificaciones antes del envío y pausar si invalidan la elegibilidad. Si aparece un archivo incompatible durante una copia ya iniciada, registrar el topic como parcial/requiere revisión y detener nuevas entregas, conservando lo copiado. No prometer ausencia absoluta de copias parciales frente a modificaciones concurrentes ni borrar mensajes para simular atomicidad. La regla «no copiar parcialmente» exige descartar desde el inicio un topic mixto conocido y comprobado; no autoriza limpieza destructiva ante cambios posteriores.

Un topic excluido permanece registrado con el motivo y sin copia ni espejo activo de topic. Si cambia su contenido, los updates o la recuperación al reconectar vuelven a evaluar TODO el topic: recibir un vídeo no lo habilita si aún conserva capítulos comprimidos. No sondear continuamente todos los topics excluidos; conservar las identidades y reutilizar el seguimiento del grupo. Si aparece un capítulo no vídeo después de una copia ya realizada, detener nuevas entregas y mostrar el motivo para revisión, sin borrar las copias existentes. Un topic destino que ya contenía vídeos no se borra automáticamente porque el origen ahora esté vacío: conservar nuestra copia según la política acordada.

## Películas y colisiones

### Presentación inicial del topic: póster y enlace TMDB

Formato aprobado tras prueba real: enviar el póster principal como FOTO y, en el pie, únicamente el enlace canónico `https://www.themoviedb.org/tv/{id}` o `https://www.themoviedb.org/movie/{id}`. Sin título, año, texto adicional ni dependencia de la vista previa del enlace. Es un mensaje de presentación, no un vídeo ni un STRM y no debe incrementar sus contadores ni entrar en el detector de episodios.

Para topics nuevos con coincidencia revisada: persistir el matcheo, crear y registrar el topic, enviar y confirmar el mensaje de presentación, y después empezar a copiar los archivos. Usar el póster principal devuelto por TMDB, no una imagen aleatoria ni el backdrop horizontal. El ejemplo validado fue un grupo privado de prueba con un topic ALF (1986); Telegram aceptó una foto enviada mediante InputMediaPhotoExternal y un pie con solo el enlace. La prueba no integra todavía esta función en el flujo de importación.

Guardar la operación y su random_id estable antes del envío; registrar el ID Telegram del mensaje enviado. Reintentos, reinicios y confirmaciones repetidas no deben duplicar la presentación. Una respuesta ambigua requiere reconciliar la entrega antes de iniciar archivos; no declarar éxito solo por haber preparado la petición. Validar que el recurso de imagen pertenece al proveedor permitido y que el enlace deriva del tipo/ID TMDB confirmado, no de una URL arbitraria del cliente.

Si falla la foto, respetar la espera Telegram y mostrar el motivo dentro del trabajo. Ofrecer reintentar o continuar sin presentación de forma explícita, sin bloquear indefinidamente ni afirmar que el póster se envió. Si no hay póster o el matcheo está pendiente, indicar presentación pendiente/no disponible; no usar una imagen de otra coincidencia. Para importación masiva, otros topics pueden continuar mientras uno requiere atención.

No insertar mensajes en topics existentes de forma silenciosa ni recrear la foto si el usuario la borró deliberadamente. Si se reutiliza un topic, comprobar si hay presentación registrada y permitir añadirla expresamente si falta; no se puede convertir una foto nueva en el primer mensaje cronológico de un topic ya poblado. Corregir el matcheo después requiere una acción revisada para actualizar la presentación propia registrada, sin editar mensajes ajenos.

En cajones de series, esta presentación corresponde a la serie del topic. En películas, corresponde al topic de una película cuando exista uno dedicado; un topic compartido con muchas películas no tiene una única coincidencia TMDB: no publicar el póster de la primera película como si representase todo el topic. Mantener este caso diferenciado en la revisión del destino y no alterar automáticamente los pies normalizados de los vídeos.

Tipo explícito por cajón/topic, nunca inferir películas por ser el primer grupo. Cajones de series: una serie por topic. Películas: reutilizar un topic de películas elegido o mantener topics separados según origen; selección visible antes de importar, sin asumir nombre «Películas» como identidad. Cada película mantiene su carpeta Título (Año), preferencias de variantes e ID TMDB ya existentes.

Misma serie desde dos fuentes: no activar dos espejos que se sobrescriban sin revisión. Ofrecer destino separado o vinculación a destino elegido con política explícita; conservar restricción actual de un espejo activo por destino hasta diseñar y probar un arbitraje. Especiales/episodios múltiples, archivos sin pie útil, duplicados y versiones mantienen las reglas actuales. Copiar un vídeo sin episodio identificado sigue siendo válido; STRM espera resolución.

## Persistencia y trabajos

### Reutilización obligatoria del sistema existente

| Responsabilidad | Base actual | Adaptación necesaria |
| --- | --- | --- |
| Recepción de cambios Telegram | internal/telegram/service.go, realtime.go y updateStorage | Mantener una sesión/runtime y el manager de gotd; ampliar alcance a grupos destino y fuentes vinculadas. Recuperar huecos por pts/seq/difference; no crear un listener por cajón o navegador. |
| Enumeración y clasificación | internal/telegram/topics.go y messages.go | Reutilizar paginación, IDs y detector de vídeos; añadir evidencia de documentos no vídeo y estado de elegibilidad completo. No deducir validez de topic a partir del contador de vídeos. |
| Copia inicial | internal/httpapi/catalog_import.go, catalog_import_queue.go e internal/telegram/imports.go | Hacer explícitos grupo destino, revisión, manifiesto y operación padre. Reutilizar leases, deduplicación y orden de episodios; eliminar resolución por título como identidad de destino. |
| Espejos y cambios de pie | internal/telegram/mirrors.go e internal/httpapi/mirrors.go | Reutilizar entregas y cola de edición; añadir barrera de elegibilidad y asociación de grupo sin un segundo motor de envíos. |
| TMDB, nombres y STRM | tmdb.go, episode_naming.go, strm_exports.go y reconciliadores | Resolver pertenencia con el servicio común de destino; mantener reglas actuales de episodios, variantes, nombres y activos estables. Póster inicial es una operación nueva acotada. |
| Interfaz y comunicación | dashboard-page.tsx, router.tsx, revisión TMDB, actividad, outbox y WebSocket existentes | Extender componentes por contexto de colección, conservar diseño y recuperación de estado desde API al reconectar. |

El modo grupo mantiene exactamente su semántica actual. El servicio de alcance resolverá destino activo para nuevas acciones y pertenencia de destinos gestionados para trabajos/vínculos existentes: cambiar carpeta activa no redirige un trabajo ya confirmado ni rompe un espejo anterior. Pausar o retirar la gestión de una colección/grupo requiere acción explícita. No ampliar automáticamente a todos los grupos de la cuenta. El modo carpeta todavía NO se activa hasta verificar paridad de todos los procesos dependientes de active_chat_id.

Evitar dos entregas para el mismo archivo durante el solapamiento de historial inicial y espejo: reutilizar un registro de entrega origen/destino e idempotencia compartida, con activación de novedades/cursor sin huecos. No ejecutar a la vez un importador y un espejo que desconozcan sus entregas mutuamente. Reintentar desde el manifiesto guardado, no rehacer una selección que crezca con nuevas búsquedas. Estado padre derivado de subtareas, sin porcentaje inventado mientras el total es desconocido; completado significa resultados verificados y excluidos explícitos, no «petición aceptada».

Las ediciones de pie del origen siguen la lógica existente y generan conciliación STRM en el destino; si el usuario modificó manualmente el pie destino desde el último valor aplicado, registrar conflicto para revisión en vez de sobrescribir silenciosamente. Los borrados en ORIGEN no eliminan copias destino. Los borrados en DESTINO conservan la limpieza actual de catálogo/STRM y no provocan recreación automática por un reintento antiguo. Mantener tombstone de la entrega borrada para evitar reenvíos involuntarios.

Migraciones nuevas, nunca editar migraciones aplicadas. Propuesta de entidades: library_collections (UUID, cuenta, nombre, Telegram filter ID nullable, estado); collection_groups (UUID, colección, grupo destino, letra, tipo, parte); group_replications (UUID, origen/destino, estado, política de descubrimiento); identidades históricas de topics y asociaciones de espejos conservadas aunque desaparezca origen. Tabla de selección de destino, separada de media_settings para evitar que un formulario antiguo sobrescriba el modo.

Unicidad por cuenta/origen/destino y asociaciones; validación de membresía, permisos y exclusión de autoespejos/ciclos. No cascadear el borrado del origen a copias ni procedencia. Revisar CASCADE actuales de mirror_links/mirror_deliveries antes de integrar. Registro de intentos de crear grupo/topic ANTES de la llamada: una operación ambigua se reconcilia, no se repite a ciegas (crear grupos no ofrece el mismo random_id que reenviar mensajes).

Reutilizar colas de importación y espejo existentes, con destinos explícitos; trabajo padre por colección y subtareas por grupo/topic, leases y reanudación después de reinicios, cursores paginados, random_id estable por entrega, orden de episodios conservado. Serializar envío dentro de cada destino; concurrencia de lectura limitada y equidad para que el volcado masivo no bloquee reproducción/espejos actuales. Transacción local + evento posterior al commit; WebSocket informa, la base de datos permite recuperar el estado si la web se cierra. Entrega de eventos al menos una vez con consumidores idempotentes y revisión monotónica; no prometer entrega exactamente una vez. Al reconectar la web, obtener estado actual para compensar mensajes WebSocket perdidos.

Telegram impone límites dependientes de cuenta y configuración. Consultar help.getAppConfig y configuración disponible, comprobar permisos actuales, manejar FLOOD_WAIT/SLOWMODE_WAIT con espera persistida exacta, CHANNELS_TOO_MUCH, contenido protegido y pérdida de acceso. No prometer 1000 archivos en milisegundos ni fijar un supuesto límite universal de envíos/minuto. La espera de métodos afectados debe coordinarse por cuenta, no provocar que otros trabajadores repitan llamadas bloqueadas. No saltarse protección de contenido, límites ni controles con sesiones paralelas. No usar for_import: corresponde a importar desde otros servicios, no a copiar entre chats Telegram. Actualizar una carpeta existente preserva sus chats, exclusiones, orden y filtros previos; no reemplazarla por una lista construida solo con nuestros cajones.

## API propuesta (pendiente de implementar)

- GET/POST /api/v1/library/collections: listar/crear colección y operación de carpeta Telegram.
- GET/PATCH /api/v1/library/target: modo grupo/carpeta, IDs validados, cambio atómico.
- GET /api/v1/library/collections/:id: grupos, cobertura y estados persistidos.
- GET/PATCH /api/v1/library/collections/:id/naming: plantillas y revisión; guardar no renombra existentes.
- POST /api/v1/library/collections/:id/naming/preview y POST .../naming/apply: previsualizar selección y confirmar revisión exacta con idempotencia; devolver trabajo de renombrado.
- POST /api/v1/library/replication-plans: guardar selección explícita de grupos desde Inicio, devolver 202 + plan ID y empezar comprobación/revisión en segundo plano; no crear chats ni enviar archivos.
- GET/PATCH /api/v1/library/replication-plans/:id: estado paginado de comprobación, exclusiones, asignaciones y coincidencias; actualizar decisiones con control de revisión. La preparación es trabajo durable y no deja una petición HTTP abierta esperando un catálogo masivo.
- POST /api/v1/library/replication-plans/:id/confirm: validar versión, carpeta y manifiestos; devolver 202 + job ID con idempotencia. Un plan obsoleto devuelve conflicto para revisar, nunca envía con la selección actual de pantalla por accidente.
- GET /api/v1/library/replications/:id y POST .../:id/pause|resume|retry: estado durable y control.
- GET /api/v1/mirrors/attention y POST .../:id/reconnect/preview|confirm: diagnóstico y reconexión revisada.
- Reutilizar endpoints existentes de lista de grupos/búsqueda para Inicio y GET/PATCH /api/v1/mirrors/:id para operaciones ya disponibles: no duplicarlos por estar en otra vista. Añadir solo contratos que no existan (colecciones, planes masivos, nombres y recuperación revisada).
- Autenticación administrativa y protección de origen existentes en todas las escrituras, JSON acotado, paginación y límites de selección. WebSocket reutiliza el canal actual; no añadir SSE ni conexión Telegram adicional por navegador. Los IDs seleccionados se validan contra la cuenta y permisos; bloquear destinos propios como fuentes del mismo plan y ciclos indirectos. Conservar el borrador tras recarga y declarar su caducidad si se purga; nunca reconstruir una selección perdida como vacía ni reejecutar un plan terminado al abrir el enlace.

## Estado verificado al retomar el 5 de octubre de 2026

Implementación actual: migraciones aditivas 037–044, alcance compartido, ajustes Grupo/Carpeta, formatos y renombrado revisado, Biblioteca de cajones, revisión durable desde Inicio/Gestión, escáner completo, cola de réplicas y espejos. Las anotaciones de primera entrega que aparecen después son históricas; este apartado registra la evidencia actual.

Prueba real controlada: carpeta Biblioteca (filter Telegram 3), cajón Series A, topic ALF (1986), seis vídeos desde Prueba1. Plan 14b5ae20-084a-4f63-b50f-ba08f9b361b9 completado; presentación enviada (mensaje 3), seis entregas verificadas y espejo activo con seis entregas sembradas. Zone continúa como grupo principal seleccionado. No se sobrescribieron STRM existentes: los episodios duplicados quedan en revisión y un vídeo sin episodio identificado no genera STRM.

Verificado: compilación web, tests Go, go vet, race en library/httpapi/telegram, upgrade completo en esquema aislado con rollback y consultas de Ajustes/Actividad contra PostgreSQL real. Revisión visual desktop y móvil 390×844 sin desbordamiento horizontal. Backup previo creado y verificado antes de aplicar las migraciones.

Pendientes de auditoría completa: cortes durante creación/foto/envío, cambios concurrentes, nuevos topics y documentos incompatibles en la colección real, recuperación asistida de origen, permisos/límites de cuenta y paridad en plataformas de despliegue. No marcar estos escenarios como comprobados solo porque el código compila.

## Tareas y auditoría por entrega

- [x] 01. Revisar restricciones actuales al grupo principal y documentación oficial.
- [x] 02. Fijar contrato, vistas, identidades, cobertura, películas, fallos y plan de API.
- [x] 03. Implementar reglas puras de letras, nombres y estados, con pruebas (internal/library). Conectadas al flujo de colección.
- [x] 03a. Adaptar nombres base a «Series N» / «Series N 2» y función de plantillas con validación y pruebas de partes/letras especiales. El manejo de nombres manuales al aplicar cambios sigue en 07a/07b.
- [x] 04b. Migración 038 de manifiestos por topic/cuenta, revisión monotónica y evidencia persistida; referencias compuestas impiden mezclar topics/grupos. Upgrade 001–038 probado en esquema aislado con rollback. Guardado conectado a planes, revalidación de updates y recuperación al arrancar.
- [x] 04. Migración aditiva de colecciones, asociaciones y selección; pruebas upgrade y preservación de datos.
- [x] 04a. Base 037: library_collections, collection_groups y catalog_targets, claves por cuenta, índices y restricciones. Auditada con todas las migraciones previas en esquema aislado y rollback, conservando grupo principal y configuración STRM. Aplicada al catálogo real junto a 038–043; upgrade aislado verificado.
- [x] 05. Servicio único de alcance del catálogo; sustituir consultas active_chat_id en TMDB, imports, realtime, artwork, reconciliación, búsqueda, espejos y STRM. Consultas de alcance, aislamiento por cuenta y enlaces antiguos auditadas; la selección previa del grupo se conserva.
- [x] 05a. Resolver común inicial: selección explícita/fallback de grupo principal y grupos destino por cuenta/colección, con pruebas aisladas de pertenencia y aislamiento. Conectado a los trabajadores mediante las vistas de alcance compartidas.
- [x] 06. Adaptador Telegram de carpetas y creación/reutilización de grupos, límites por cuenta consultados en help.getAppConfig y operaciones ambiguas recuperables mediante marcador de operación y revisión explícita. No reescribe la carpeta cuando la asociación ya coincide.
- [x] 07. API de configuración + selector Grupo/Carpeta y crear carpeta; auditoría escritorio/móvil y recuperación tras error.
- [x] 07a. Ajustes de formato de nombres con ejemplos; persistir revisión y distinguir nombres automáticos/manuales sin renombrar al guardar.
- [x] 07b. Vista previa y selección para aplicar nombres existentes; trabajo durable con progreso, conflictos externos, permisos, espera Telegram y reintentos. Auditar que no modifica topics, origen ni STRM.
- [x] 08. Biblioteca de cajones y cobertura; rutas y pertenencia, sin cambiar enlaces multimedia existentes.
- [x] 09. Inicio → Gestión: seleccionar cajones externos encontrados con búsqueda, todos los resultados paginados o individuales; revisión durable por plan con carpeta destino, películas/series, colisiones, variantes y exclusiones. Reutilizar componentes de selección/revisión TMDB y conservar borrador/ruta tras recarga.
- [x] 09b. Elegibilidad del TOPIC COMPLETO antes de crear destinos: escanear todas las páginas, incluidos documentos no vídeo, y excluir todo el topic si algún capítulo es ZIP/RAR u otro formato no vídeo. Sin copias parciales. Diferenciar pendiente/error/excluido/revisión/elegible y reevaluar todos sus archivos tras cambios; imágenes y presentación no excluyen.
- [x] 09b-escáner. Servicio de lectura completa de historial con la sesión existente: avanza hasta una página vacía, rechaza cursores repetidos y respuestas no verificadas, conserva evidencia de vídeos/documentos y excluye comprimidos incluso en páginas posteriores. Pruebas de interrupción, documentos dudosos, presentación y vídeos enviados como documento. Conectado a manifiestos y orquestador; réplica pequeña real verificada.
- [x] 09b-base. Reglas puras de elegibilidad y pruebas: 10 vídeos + 1 capítulo no vídeo excluye todo el topic, escaneo incompleto/cambiado no habilita y documentos dudosos requieren revisión. Conectadas al escáner completo y a la barrera previa al envío.
- [x] 09a. Presentación de topics nuevos: foto del póster principal + solo enlace TMDB en el pie, confirmada antes de copiar archivos; persistencia de random_id/ID de mensaje, reintentos sin duplicar y tratamiento explícito de falta de póster, fallos y topics reutilizados/compartidos.
- [x] 10. Orquestador durable de copia masiva usando colas existentes; progreso, pausas, reintentos y reinicios sin duplicar.
- [x] 10a. Unificar deduplicación/cursor de copia inicial y novedades de espejo; impedir doble envío durante solapamiento, coordinar esperas por cuenta y verificar entregas antes de declarar éxito.
- [x] 11. Espejos automáticos por topic y descubrimiento de nuevos topics, edición de pies y STRM con todos los destinos autorizados.
- [x] 11a. Resolver conflictos de pies corregidos manualmente; mantener tombstones de borrados destino y separación de borrados origen/destino. Cambiar modo/carpeta no redirige trabajos ni destruye vínculos existentes.
- [x] 12. Filtro de desvinculados, diagnóstico y reconexión manual asistida; conservar copias al borrar origen.
- [ ] 13. Auditoría integral: renombres, eliminaciones, acceso perdido, límites, cortes durante creación/envío, múltiples fuentes, nuevos topics, ficheros documento/vídeo y reproducción durante importación.
- [ ] 13a. Auditar presentación TMDB: orden foto antes de capítulos, una sola foto tras reinicios/reintentos, pie solo URL, corrección revisada de coincidencia y exclusión de contadores de vídeo/STRM; verificar series y películas con destino dedicado o compartido.
- [ ] 13b. Auditar elegibilidad: 10 vídeos + 1 capítulo ZIP debe excluir TODO el topic; probar comprimidos en última página, solo comprimidos, imágenes/presentación junto a vídeos, documentos dudosos, vídeo como documento, vídeo sin datos de episodio, escaneo interrumpido y cambios durante revisión. Ningún destino/póster/copia se crea antes de completar la comprobación; recibir un vídeo no habilita un topic que conserva capítulos no vídeo. Si el impedimento aparece después, pausar nuevas entregas sin borrar copias existentes.
- [ ] 14. Prueba controlada pequeña antes de volcado real; no usar un grupo completo como primera prueba. Revisar backups/migraciones, Linux/macOS/Windows/Docker y documentación.


## Auditoría del plan actualizado

Revisión de arquitectura realizada sobre rutas, colas y espejos actuales. Corregido: selección de fuentes desde Inicio (no Biblioteca), preparación asíncrona recargable, historial completo para excluir topics mixtos, destinos explícitos en vez de títulos, solapamiento copia/espejo, eventos recuperables y preservación de configuración previa de carpetas. La implementación está conectada y la réplica pequeña ya se verificó. Los escenarios adversos pendientes se mantienen separados en 13–14; no se presentan como comprobados.

Antes de activar: pruebas en modo grupo existente y luego colección pequeña con renombre, desconexión, reinicio entre foto/archivos, ZIP al final, cambio durante copia, edición manual destino, múltiples pestañas, cambio de carpeta activa y borrado destino. Los fallos de origen o permisos no deben causar limpieza de nuestras copias. La auditoría visual de esta entrega incluye revisión y Biblioteca en escritorio/móvil; los cortes adversos en operaciones remotas siguen pendientes.

Auditoría de la primera entrega: se detectó que library_targets ya existía desde la migración 010 para destinos físicos STRM. La selección nueva se llamó catalog_targets para no interferir con esa tabla. Las pruebas recorren las migraciones 001–036 y luego 037 en un esquema transaccional de prueba; verifican preservación de la selección anterior y rutas STRM, referencias cruzadas entre cuentas rechazadas, no activación automática y rollback de todo el fixture. Las nuevas tablas no activan servicios ni reenvíos. Las migraciones antiguas permanecen intactas.

Hallazgos iniciales resueltos: el alcance compartido sustituye las restricciones al antiguo grupo principal; Espejo admite destinos administrados de las colecciones; los vínculos conservan procedencia y tombstones. active_chat_id permanece únicamente como compatibilidad de selección anterior, sin redirigir trabajos persistidos.

## Fuentes oficiales consultadas

- Carpetas, asociación de chats y updates: https://core.telegram.org/api/folders
- Forums y topics: https://core.telegram.org/api/forum
- Configuración y límites por cuenta: https://core.telegram.org/api/config
- Crear grupo foro y errores: https://core.telegram.org/method/channels.createChannel
- Crear/actualizar carpeta: https://core.telegram.org/method/messages.updateDialogFilter
- Reenvíos, random_id, destino topic y restricciones: https://core.telegram.org/method/messages.forwardMessages
- Foto desde URL y envío con pie: https://core.telegram.org/constructor/inputMediaPhotoExternal y https://core.telegram.org/method/messages.sendMedia
- FLOOD_WAIT, errores y permisos: https://core.telegram.org/api/errors
- Secuencias de updates y recuperación de huecos: https://core.telegram.org/api/updates
- Enumeración paginada de topics y mensajes del hilo: https://core.telegram.org/method/messages.getForumTopics y https://core.telegram.org/method/messages.getReplies
- Editar título propio y pie de mensaje: https://core.telegram.org/method/channels.editTitle y https://core.telegram.org/method/messages.editMessage

Las decisiones de nombres, cobertura alfabética, rutas y reconexión revisada son diseño de TgStrm; no convenciones obligatorias de Telegram.

Auditoría del escáner completo: usa messages.getReplies sin filtro de vídeo; una página corta no cierra la comprobación. Se comprueban páginas posteriores y errores de progreso, sin crear chats ni escribir mensajes. La clasificación utiliza videoMetadata existente. Un documento desconocido queda en revisión; no se descarta para autorizar una copia parcial. El manifiesto está persistido, se invalida con updates y se comprueba nuevamente antes de crear el destino o copiar archivos.

Auditoría adicional: migración 038 verificada sobre las migraciones reales anteriores en PostgreSQL local, dentro de una transacción aislada revertida. Las rutas físicas STRM y la selección del grupo previo permanecen iguales. El guardado de manifiestos valida pertenencia por cuenta/grupo/topic y aumenta revisión; no autoriza entregas. Invalidación por updates, revisión exacta y conexión con planes durables implementadas. La comprobación de los límites usa configuración oficial de la cuenta y no trunca cajones al alcanzar un límite.

## Cierre de la revisión retomada

- Enlaces antiguos: comprobados contra PostgreSQL real y en navegador. Un enlace de topic guardado resuelve su colección/grupo real aunque cambie el destino seleccionado; los errores de base de datos no se disfrazan de «topic eliminado».
- Eventos: bloqueo/desbloqueo de espejos, origen/destino no disponible y nuevos topics publican eventos en la misma transacción que el cambio. Las sentencias se validaron con EXPLAIN contra el esquema actualizado, sin ejecutar esas modificaciones.
- Borrados: prueba transaccional revertida sobre la réplica controlada. Borrar origen no marca la copia como eliminada; borrar destino deja tombstone tanto en espejo como en importación. No se eliminaron mensajes de Telegram durante esta auditoría.
- Interfaz: Espejo informa de errores al crear/eliminar vínculos, descarta respuestas antiguas del selector y muestra un estado vacío coherente al filtrar atención. Conserva los componentes y tamaños existentes.
- Límites: defaults oficiales 10/30 carpetas y 100/200 chats, sustituidos por configuración de la cuenta cuando existe. Pines no se cuentan dos veces. Se pausa con diagnóstico, sin truncar contenido ni borrar carpetas existentes. Telegram sigue siendo la autoridad final para permisos y límites.
- Verificación: tests Go, go vet, race, build web y auditoría SQL local pasan. El build conserva una advertencia de tamaño del bundle; no es un error de compilación.

Las tareas marcadas como implementadas no acreditan pruebas de estrés ni ejecución en Linux/Windows/Docker. Los escenarios remotos adversos y la paridad completa de despliegue siguen expresamente pendientes en 13–14. El caso pequeño se completó y sobrevivió a reinicios sin duplicar su presentación ni sus seis entregas.

### Prueba de uso de esta entrega

1. Ajustes → Biblioteca: crear o elegir carpeta; no es obligatorio sustituir el grupo principal para preparar una réplica controlada.
2. Inicio → Gestión → Seleccionar cajones: buscar un grupo externo pequeño, elegir la carpeta y pulsar «Revisar cajones».
3. Comprobar topics, letra, coincidencias y exclusiones; confirmar solo cuando el análisis completo haya terminado.
4. Abrir Biblioteca en otra pestaña y comprobar presentación, archivos y espejo. El proceso continúa al salir de la revisión.
5. Para la primera prueba usar un grupo propio pequeño, no los cajones masivos. Un topic con ZIP/RAR queda excluido entero; no se copian únicamente sus vídeos.

Renombrado revisado: al terminar un cambio de nombres se publican tanto el evento del catálogo como el de Biblioteca, para actualizar los cajones sin recargar la página.

Auditoría final de reintentos: migración 044 aplicada después de probar el upgrade aislado. Los manifiestos fallidos se vuelven a comprobar con un próximo intento persistido; el descubrimiento de topics conserva su estado ante fallos temporales y respeta FLOOD_WAIT. Los errores de permisos siguen requiriendo revisión. Pruebas del intervalo exacto (137 y 600 segundos), SQL, race y go vet pasan.

Compilación de esta revisión verificada para Linux amd64, Windows amd64 y macOS arm64, además de la ejecución de desarrollo en macOS Intel. Esto confirma portabilidad de compilación, no ejecución real en las otras plataformas ni despliegue Docker. No se ha publicado una release nueva.

## Imágenes identificativas de los cajones — 2026-10-05

- [x] JPEG real de 512×512 compuesto en Go con los pósters TMDB: mosaico denso, columnas escalonadas, giro de 14° y perspectiva suave. Se recorta circularmente en el avatar existente, sin alterar las dimensiones de las tarjetas.
- [x] Selección por título `es-ES` e inicial normalizada (acentos, Ñ y números). Orden `popularity.desc`, IDs únicos, hasta 12 pósters. TMDB no ofrece filtro por inicial: la búsqueda recorre como máximo 40 páginas compartidas; las letras escasas pueden tener menos pósters y reutilizarlos en la composición. No se afirma una cobertura ilimitada del catálogo.
- [x] Endpoint autenticado por colección/miembro. Credenciales solo en servidor. Caché atómica persistente en `data/cache/cabinet-artwork`, siete días; catálogo compartido durante 24 horas. Respeta el limitador y 429 existentes. Fallos sin imagen tienen diez minutos de espera; conservan el icono. Sin cambios en Telegram, espejo, exportaciones o estructura de STRM.
- [x] Auditoría: iniciales y deduplicación, rechazo de rutas de póster no válidas, todos los píxeles cubiertos, integración local con PostgreSQL/TMDB y validación JPEG. Imagen de Series A generada y examinada. Build web, pruebas Go y race pasan.

Fuentes: [Discover TV](https://developer.themoviedb.org/reference/discover-tv), [imágenes TMDB](https://developer.themoviedb.org/docs/image-basics) y [límites TMDB](https://developer.themoviedb.org/docs/rate-limiting).

Auditoría visual de los avatares completada en escritorio y a 390×844: imagen cargada, 42×42 en ambos ejes y ancho documental igual al viewport (sin desbordamiento). La caché está ignorada por Git. El servicio de desarrollo mantiene la instalación existente.

### Corrección: foto del grupo real dentro de la carpeta

- [x] El mosaico ya no se limita al avatar web: una tarea persistente lo sube al grupo de destino mediante `channels.editPhoto` e `inputChatUploadedPhoto`. Se aplica a los cajones existentes sin foto y a los nuevos, cuando la carpeta queda preparada.
- [x] Migración 045 aditiva: estado, ID de foto confirmado, diagnóstico y próximo intento. Un fallo decorativo no detiene importaciones ni espejos; los reintentos respetan FLOOD_WAIT y tienen como mínimo diez minutos de espera.
- [x] Comprueba Telegram antes de subir. Conserva fotos previamente asignadas y reconcilia respuestas inciertas: reiniciar tras aceptar la foto no repite la subida. Excluye cajones/destinos/carpetas inactivos o archivados.
- [x] Mismo generador/caché para web y Telegram. SQL y migración comprobados en transacción revertida; pruebas Go, race y go vet pasan.
- [ ] Verificar una subida real en la siguiente réplica del usuario: el cajón local Series A figura como destino eliminado, por lo que no se recrea ni se utiliza para probar envíos. No se afirma que una foto haya sido subida a ese grupo eliminado.

Fuente oficial: [cambiar foto de supergrupo](https://core.telegram.org/method/channels.editPhoto) y [subida de archivos e imágenes](https://core.telegram.org/api/files).

## Limpieza para cambiar de organización — 2026-10-05

- [x] Ajustes → Biblioteca principal → Revisar limpieza. Muestra nombre del destino **guardado**, cantidad de STRM, carpetas y ubicaciones. Requiere escribir `LIMPIAR`; una huella de IDs/rutas/revisión rechaza confirmaciones antiguas.
- [x] Conserva Telegram, catálogo, TMDB, assets/URLs estables, credenciales, rutas y organización. Quita las carpetas completas identificadas por las exportaciones del destino y sus filas `stream_exports`, y reinicia sus marcas de procesamiento. La limpieza no borra archivos ajenos no registrados ni las raíces de biblioteca.
- [x] Pausa automática persistente por grupo del destino anterior. Las nuevas colecciones/grupos no heredan esa pausa. Generar explícitamente o reactivar desde Ajustes sigue disponible; las pausas automáticas no convierten una importación completada en fallo.
- [x] Migración 046 aditiva: intención y plan de limpieza, progreso por carpeta, lease, atención y reanudación tras reinicio. El trabajo continúa fuera de la página. Borrar y después quitar registros en una transacción por carpeta permite repetir tras un corte sin afirmar éxito si el disco falló.
- [x] Protecciones: exclusión con escrituras/reconciliación, rechazo de raíz completa y rutas no locales, `os.OpenRoot`/`Root.RemoveAll`, resolución de rutas relativas y alias, bloqueo de carpetas compartidas con otro destino, trabajo único activo por cuenta. No se borran grupos ni mensajes Telegram.
- [x] Auditoría: fixtures temporales (carpeta con temporada/póster, vecino y raíz conservados, idempotencia, symlink externo y traversal). Vista previa PostgreSQL real, EXPLAIN del claim y rechazo de confirmación antigua. Tests, race, go vet y build web pasan. Vista a 390×844 sin desbordamiento y confirmación inicialmente deshabilitada.
- [ ] La eliminación de la biblioteca real la confirma el usuario; no se ha ejecutado durante esta entrega. La auditoría remota no equivale a una prueba de corte físico del equipo durante el borrado.

Fuentes: [PostgreSQL: transacciones](https://www.postgresql.org/docs/current/tutorial-transactions.html), [Go: os.Root y RemoveAll](https://pkg.go.dev/os#Root.RemoveAll).

### Auditoría de revisión de réplica (05-10-2026)
- [x] Conservar borrador y selección durante la carga paginada; evitar refrescos parciales que reemplacen la lista completa.
- [x] Explicar las coincidencias pendientes que bloquean confirmar; selección individual y conjunta con revisión transaccional.
- [x] Mostrar revisión TMDB en Dialog de Headless UI, con foco y cierre accesibles, en escritorio y móvil.
- [x] Uniformar las tarjetas y reservar el espacio de títulos y coincidencias.
- [x] Separar progreso de análisis del de replicación; mostrar cuenta regresiva FLOOD_WAIT basada en next_attempt_at.
- [x] Migración 047: archivar cajones eliminados y liberar sus partes con índice único parcial, conservando historial.
- [x] Verificado con borrador de 126 topics: misma altura, búsqueda TMDB y deselección persistente; pruebas SQL revertidas de selección conjunta y reutilización de Parte 1. No se confirmó ni envió la réplica durante la auditoría.
- [x] go test ./..., go vet del API y compilación web correctos.

### Selectores y referencias para coincidencias (05-10-2026)
- [x] Tipo, letra y parte agrupados en una barra con Listbox de Headless UI, compatible con tema y móvil.
- [x] Contraste de botones y distinción de la confirmación principal; sin modificar las elecciones guardadas.
- [x] Conservar referencias acotadas de previews y enlaces de Telegram en la evidencia JSON existente. No se consultan URLs arbitrarias.
- [x] Enlaces TMDB e IMDb resueltos mediante API oficial; FilmAffinity utiliza título/año de la preview de Telegram, nunca su ID como ID de TMDB.
- [x] Coincidencia automática solo si es única; referencias contradictorias requieren revisión manual.
- [x] Acción voluntaria Mejorar coincidencias: vuelve a analizar solo series incluidas sin match, preservando las coincidencias existentes y exclusiones.
- [x] Tests: título/año, ambigüedad, metadata de Telegram, enlaces ocultos, límites y rechazo de hosts suplantados; pruebas completas, race y vet correctos. UI comprobada a 390 px sin desbordamiento.
- [ ] Validar el resultado de FilmAffinity con referencias reales cuando se use la nueva acción; los borradores anteriores no contenían esa metadata.

### Corrección estética de revisión (05-10-2026)
- [x] Retirar fondos blancos nuevos y separadores de los selectores; usar superficies suaves y acciones transparentes existentes.
- [x] Confirmación con clase explícita y colores correctos en tema claro/oscuro, sin depender del icono para decidir su estilo.
- [x] Corregir contraste del checkbox y estado de revisión en oscuro; input TMDB con fila completa en móvil.
- [x] Auditoría en navegador: tarjetas uniformes, menús acotados, ventana TMDB y selección; 390 px sin desbordamiento. No se modificó el borrador ni se reinició el backend.

### Revisión visual de topics — 2026-10-05
- [x] Filtros locales: todos, asociados, sin asociar y excluidos; búsqueda por título original o coincidencia TMDB y contador visible.
- [x] Selección individual compacta en la esquina superior con nombre accesible específico por topic; sin texto repetido.
- [x] Tarjetas compactas y botón de revisar alineado en una fila inferior; sin reservar altura por el texto de selección eliminado.
- [x] Verificados en navegador: filtro vacío sin asociar, búsqueda ALF (3 resultados), selección conservada (117 incluidos), escritorio y móvil de 390 px sin desbordamiento horizontal; primeras tarjetas móviles de 148 px de alto.
- [x] Compilación TypeScript/Vite y revisión de espacios pasan. Cambios limitados al frontend de revisión; sin envío, modificación de coincidencias ni acciones de Telegram durante la auditoría.
