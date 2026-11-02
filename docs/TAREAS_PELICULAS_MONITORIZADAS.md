# Películas monitorizadas

## Contrato e interfaz

El origen se marca explícitamente como películas: grupo sin topics, topic o cajón con topics. El destino admite el grupo Películas sin topics de una carpeta o el topic de películas del modo grupo principal. Series y espejos existentes conservan su comportamiento.

Inicio / Gestión ofrecerá «Monitorizar películas». Espejo / Gestión mostrará las fuentes, destino, pausa y pendientes en un apartado Películas. Se reutilizan buscadores, navegación y desplegables existentes. Revisión mostrará archivo original, pie, posible coincidencia, título español, año y póster. Confirmar libera solo ese archivo. Actividad agrupa trabajos y muestra errores accionables y reanudación de FLOOD_WAIT. Configuración define destino y política de versiones.

Cada vídeo se identifica individualmente ANTES de enviarlo. Vídeos nativos y documentos de vídeo usan la clasificación actual. ZIP/RAR y mensajes normales no se envían; no se cambia la regla que excluye topics de series con capítulos comprimidos.

Un match incierto queda en revisión; una caída de TMDB se reintenta. Nunca usar el nombre del grupo como identidad de todos sus archivos. No ejecutar pruebas de envío/borrado sobre la biblioteca real.

## Flujo y seguridad

`descubierto → identificando → revisión / listo → presentación → archivo → indexación → STRM → completado`

Presentación: foto del póster con título español, año y enlace TMDB antes del vídeo. No cambiar el avatar del grupo por cada película. El vídeo lleva pie normalizado. Persistir foto y vídeo por separado, con identificadores de deduplicación diferentes. No completar la entrega después de enviar solo el póster.

Identidad de origen: peer/topic/message ID. Identidad de película: movie/TMDB ID. IDs estables, leases y reanudación tras reinicio. No borrar las copias porque desaparezcan en origen. No recrear automáticamente un destino borrado. Evitar bucles, autoespejos y monitorización implícita de otros grupos.

La búsqueda aprovecha pie y nombre; contradicciones y varios candidatos requieren revisión. Consultar detalles TMDB en es-ES tras elegir el ID. No aceptar por popularidad el primer resultado ambiguo.

## Versiones

Conservar vídeos de distintas calidades/formatos; no aplicar la sustitución de episodios a películas. Carpeta `Título (año)` y varios STRM con el mismo prefijo y sufijo de versión. Calidad/códec solo cuando existe evidencia; identificador estable evita colisiones entre versiones de igual calidad. ID TMDB en carpeta sigue siendo opcional. Los archivos del monitor usan nombres compatibles con las múltiples versiones de Jellyfin, con sufijo estable por asset. El perfil global y las series no cambian.

## Tareas

- [x] Arrancar desarrollo; subdominio únicamente en variable de entorno.
- [x] Revisar espejos, importador, presentación y rutas de versiones existentes.
- [x] Consultar fuentes oficiales.
- [x] Endurecer el match automático: varios IDs con igual título/año se consideran ambiguos.
- [x] Registro de fuentes/destinos group/topic/collection con clasificación películas.
- [x] Migración aditiva: cola por archivo, fases independientes, leases e índices de deduplicación.
- [x] Admitir grupos sin topics en catálogo, importador, presentación y exportación sin crear topics reales.
- [x] API autenticada: listar, configurar, pausar, revisar y confirmar; comprobar biblioteca y evitar bucles.
- [x] Descubrimiento por updates y recuperación paginada al arrancar, solo de fuentes seleccionadas.
- [x] Worker de identificación previa por pie/nombre, detalle TMDB, revisión y reintentos.
- [x] Envío póster/título/enlace + vídeo normalizado, FLOOD_WAIT y recuperación entre fases.
- [x] Reutilizar match destino, generación STRM y reconciliación física; conservar múltiples versiones.
- [x] UI Inicio/Gestión, Espejo y revisión con los componentes actuales; 320/390/1440 px y teclado.
- [x] WebSocket, Actividad agrupada y recuperación de estado tras reconectar.
- [x] Regresiones: carpeta/grupo, group/topic origen, vídeo/documento, ambigüedad, varias versiones, reinicios, ediciones, destino borrado y fuente inaccesible.
- [x] Auditoría final y documentación; pruebas remotas solo con autorización específica.

## Restricciones encontradas

El importador exige destino foro, la presentación tiene unicidad por topic y la cola agrupa por topic origen/destino. Estas tres restricciones deben adaptarse: un grupo de películas tiene muchas identidades y entregas independientes. No basta convertir un espejo de serie en movie.

## Fuentes oficiales

- [TMDB: búsqueda y detalles](https://developer.themoviedb.org/docs/search-and-query-for-details)
- [Jellyfin: organización y múltiples versiones](https://jellyfin.org/docs/general/server/media/movies/)
- [Telegram: sendMedia](https://core.telegram.org/method/messages.sendMedia)
- [Telegram: updates y random_id](https://core.telegram.org/api/updates)
- [Telegram: errores y FLOOD_WAIT](https://core.telegram.org/api/errors)

Implementación habilitada. El volcado inicial continúa en segundo plano; las coincidencias dudosas requieren revisión humana. Las comprobaciones internas no equivalen a probar destructivamente todas las variantes con Telegram real.

## Registro histórico de la primera fase (superado por la auditoría final)

- Migración 053 definida y probada dentro de una transacción revertida. Monitores nuevos pausados por defecto; barreras de estado requieren match y confirmación antes de presentar/enviar, y el ID de presentación antes del vídeo. No se ha aplicado al servicio en ejecución todavía.
- Regresión del match: mismo título/año con IDs distintos requiere revisión; título original, año discriminante y duplicación del mismo ID siguen funcionando.
- Corregido lector de entregas del espejo: había un alias SQL inexistente (`t`) y no seleccionaba el mensaje por `md.source_message_id`. Ahora identifica exactamente el mensaje de la entrega. La consulta real se comprueba en PostgreSQL sin realizar envíos.
- `go test -race ./...` con PostgreSQL local: correcto antes de añadir la migración; tests específicos de esquema y match después: correctos. `go vet ./...`: correcto.
- No se ejecutaron envíos ni borrados de prueba, ni se crearon monitores activos. Los servicios levantados mantienen los trabajos existentes habituales.
- La nueva interfaz y el worker de películas aún están pendientes: no presentar esta fase como funcionalidad completa.


## Auditoría final · 6 de octubre de 2026

- Migraciones 053–055 aditivas aplicadas; copia de seguridad local previa con permisos privados. Cola durable con leases, firmas de origen y barreras de estado verificadas en PostgreSQL.
- Origen solicitado vinculado al grupo **Películas**, sin topics reales, dentro de la carpeta principal. Escaneo inicial terminado: **5.387 vídeos/documentos de vídeo**. Identificación y envío siguen activos; no se afirma que el volcado haya terminado.
- Más de 30 entregas completas comprobadas durante la auditoría. Ninguna entrega completada tiene presentación ausente o posterior al vídeo. Tres STRM físicos contrastados con su hash registrado: correctos.
- Reinicio de API comprobado: recuperación del progreso persistido. Los 114 espejos activos de series siguen presentes.
- Identificación independiente de las esperas de envío; FLOOD_WAIT conserva su plazo y muestra cuenta regresiva. Las fases de exportación fallidas no paralizan todo el destino.
- Escaneo separado de vídeos y documentos, paginación persistida y publicación de snapshots para conteos de topics/archivos. WebSocket con recuperación periódica de la interfaz.
- Selección automática del topic de películas solo si existe exactamente uno; varios destinos requieren selección explícita, nunca elegir el primero arbitrariamente.
- UI comprobada a 320, 390 y 1440 px. Corregido ancho mínimo de la cuadrícula, encabezados móviles y mensajes técnicos de respuestas vacías durante reinicios.
- `go test -race ./...`, `go vet ./...`, pruebas PostgreSQL de consultas y restricciones: correctos. Compilación web correcta; permanece el aviso previo de tamaño del bundle.
- Cobertura interna: tipos de origen/destino, barreras, ambigüedad, formato de presentación y esperas. Comprobación remota autorizada: carpeta/grupo Películas y origen seleccionado. No se han eliminado archivos ni realizado pruebas destructivas sobre series.

### Operación

En **Espejo → Gestión → Películas** se ven STRM disponibles, pendientes, revisión y pausa. «Revisar archivos» permite buscar una coincidencia TMDB y confirmarla por archivo. Eliminar un archivo en origen no elimina su copia del destino. El procesamiento continúa sin mantener abierta la web; PostgreSQL conserva el progreso si la API se reinicia.


## Corrección de bloqueo por póster · 7 de octubre

`WEBPAGE_CURL_FAILED` mantenía una entrega en presentación y bloqueaba las siguientes. Se añade subida directa como alternativa a la descarga remota de Telegram, usando el mismo random_id. Descarga únicamente desde TMDB, sin redirecciones, limitada a 8 MiB y 15 segundos, validando imagen JPEG/PNG y sin almacenar caché en disco. Los fallos definitivos previos al envío conservan el match y vuelven a preparación, permitiendo avanzar otras películas. Los timeouts de aceptación incierta siguen bloqueados hasta reconciliar para evitar duplicados. No se cambió la presentación ni se borró contenido. El indicador prioriza el error reciente y sustituye una espera vencida por estado de reintento.

Pruebas: validación de imágenes, tamaño y destino; fallo definitivo frente a aceptación incierta; consultas PostgreSQL, compilación web, test con race detector y vet correctos.
