# Grupos sin topics y copia manual fuera de Biblioteca

Fecha: 2026-10-25.

## Causa y alcance
El grupo 103 (The Blacklist) es un supergrupo/canal sin foro. No tenía el registro local telegram_topic_id=0. La vista ya sabía entrar a ese historial si existía y el lector ya usaba messages.getHistory para ID 0, pero groupTopics no creaba el registro al abrir un grupo normal. La acción de catálogo, además, restringía el destino a Biblioteca.

## Correcciones
- Al abrir un grupo sin foro se prepara su ámbito de historial local mediante EnsureWholeGroupTopic(false). No se crea un topic en Telegram y no se cambia a colección de películas.
- Ese ámbito también admite grupos básicos (InputPeerChat). Los lectores interactivo y de filtros resuelven el InputPeer según el tipo de grupo.
- Se reutilizan la vista, paginación, clasificador de vídeo/documento de vídeo, selección y watch existentes.
- Nueva acción «Enviar a otro grupo», disponible para todo el historial o una selección. Permite buscar grupo y, si es foro, buscar un topic existente. No crea grupos ni topics remotos.
- Cola persistente file_transfers separada del catálogo: destino explícito, selección completa e inmutable, progreso, espera FLOOD_WAIT y visibilidad en Actividad. La migración 061 es aditiva.
- Los mensajes se reenvían en lotes de hasta 100, ordenados por ID de Telegram ascendente; se conserva el pie de archivo y no se descarga/sube el vídeo. No hay asociación TMDB, STRM ni espejo añadidos al destino por este flujo.
- Los IDs aleatorios dependen del trabajo y mensaje. Solo se contabiliza un lote si Telegram devuelve todas sus confirmaciones UpdateMessageID.
- Una interrupción o aceptación incierta deja el trabajo en atención; no se repite automáticamente un lote que podría estar entregado. El usuario puede inspeccionar el destino y detener el trabajo pendiente/en atención antes de preparar una selección nueva. No se permite cancelar un lote en ejecución.
- Valida cuenta, grupo/topic activo, pertenencia del topic al destino, selección de vídeos, destino distinto del origen y petición all/selección inequívoca. Una petición repetida mientras existe un trabajo pendiente al mismo destino devuelve conflicto.
- La escritura sigue sujeta a permisos y protección de contenido de Telegram; un rechazo se guarda y se presenta al usuario, no se elude.

## Verificación
- go test ./..., go vet ./... y compilación Go: correctos.
- npm run build: correcto. Persiste el aviso previo de tamaño de bundle >500 kB.
- TestForwardedFileBatchRequiresEveryConfirmation: lote confirmado, aceptación parcial, actualizaciones ajenas y actualizaciones no disponibles.
- TestDirectGroupHistoryAndTransferValidationDatabase: apertura real del grupo 103, ámbito ID 0, destinos inválidos, copia al mismo origen, selección vacía/ambigua y SQL de Actividad.
- TestFileTransferLeaseAndManifestDatabase: reclamación persistente del trabajo, identidad y progreso. Datos de prueba revertidos con rollback; nunca visibles para el worker.
- API reiniciada y accesible. Grupo 103: ámbito local 13188020, 218 vídeos cargados, library_kind unclassified. Cero filas en file_transfers tras las pruebas.
- No se enviaron mensajes de prueba ni se borró contenido de Telegram. La auditoría visual autenticada posterior confirmó los 218 vídeos (183,22 GB), búsqueda de grupo, carga de topics de Zone y habilitación del botón al elegir un topic. Se cerró sin enviar.

## Fuentes oficiales
- https://core.telegram.org/method/messages.getHistory : historial del peer sin requerir foro.
- https://core.telegram.org/method/messages.forwardMessages : IDs de origen, destino, top_msg_id opcional y random_id.
- https://core.telegram.org/api/errors : respetar FLOOD_WAIT y errores de permisos.

## Uso
Abrir /groups/103 → Gestión → «Enviar a otro grupo» para todo el historial, o «Seleccionar archivos» y después «Enviar a otro grupo» para una selección. Elegir grupo; si usa topics, elegir uno existente. «Confirmar envío» guarda el trabajo antes de procesarlo. El progreso persiste al salir de la web y aparece en Actividad.

## Auditoría visual con sesión iniciada

Se detectó que montar el diálogo dentro de la barra sticky heredaba su recorte y estilos de botones. Se movió mediante createPortal a la raíz .dashboard-page, conservando el tema y el patrón de los otros diálogos. Se verificó en navegador que la cabecera, el fondo, los selectores y el pie se muestran completos. Seleccionar un destino habilita Confirmar envío; cerrar el cuadro no crea trabajos. Compilación web repetida correctamente. Captura local: /tmp/tgstrm-envio-grupo-verificado.png.
