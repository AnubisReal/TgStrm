# Envío manual de series a destinos del catálogo

Fecha: 2026-10-15.

## Problema
La revisión manual enviaba un topic destino, pero no permitía elegir el grupo para crear uno nuevo. Con varios cajones en una carpeta, la cola no podía deducir el destino. Además, una solicitud podía devolver un trabajo en curso diferente o sustituir la selección de uno pendiente.

## Cambios
- La revisión ofrece grupo destino, topic existente o creación/reutilización de topic con la identidad TMDB. Los desplegables usan PillSelect (Headless UI) y el topic tiene filtro de búsqueda.
- Sugiere la letra del título seleccionado cuando existe un único cajón correspondiente; la elección manual prevalece. No crea grupos ni decide entre varias partes de una letra.
- El endpoint admite destination_group_id, conserva destination_topic_id y message_ids, y valida pertenencia al catálogo activo y a la misma cuenta antes de consultar el historial de Telegram.
- Mantiene el modo antiguo de grupo único y el modo carpeta. Permite enviar desde un grupo ya gestionado a otro topic, pero bloquea origen y destino idénticos en API/cola/worker.
- Los topics asociados a otra serie no se reasocian por enviarles archivos. La cola vuelve a validar la identidad dentro de su transacción. Para crear/reutilizar, busca también el ID TMDB dentro del grupo elegido.
- Una solicitud repetida solo reutiliza el trabajo pendiente si grupo, topic, título, asociación y selección coinciden. Otra solicitud devuelve conflicto en lugar de redirigirlo o reiniciarlo.
- Se reutilizan la cola, manifiestos, identificadores estables, generación STRM y procesamiento de destino existentes. No se introduce una nueva ruta de copiado ni migración de datos.
- Se comprueba el error final de lectura de mensajes para no aceptar un historial parcialmente leído.

## Verificación
- go test ./... y go vet ./...: correctos.
- npm run build: correcto; persiste el aviso previo de bundle mayor de 500 kB.
- TestManualImportDestinationsDatabase: consultas de solo lectura sobre el catálogo real para grupo explícito, compatibilidad del formato antiguo, creación, grupo/topic inexistentes o incompatibles, contenido incorrecto, autorreenvío y destino ambiguo.
- TestManualImportPendingSelectionDatabase: compara una solicitud almacenada y rechaza cambios de grupo o selección, sin crear trabajos.
- No se han enviado archivos ni creado topics de prueba en Telegram. No se ha verificado visualmente con una sesión autenticada en este turno; no había pestañas abiertas.

## Fuentes oficiales
- https://core.telegram.org/method/messages.forwardMessages: grupo destino, topic destino y random_id para evitar reenvíos duplicados.
- https://core.telegram.org/api/forum: relación entre grupos y topics.
- https://core.telegram.org/method/channels.createForumTopic: creación y restricciones del foro. La implementación conserva el método compatible con la capa del SDK instalado.

## Prueba funcional del usuario
Seleccionar los capítulos de Juego de tronos en Zone → Enviar al catálogo → confirmar ficha TMDB → seleccionar Series J → elegir topic existente o crear/reutilizar → Confirmar y enviar. La selección de capítulos se conserva. El destino debe ser un grupo con topics ya incorporado al catálogo; crear un cajón nuevo sigue siendo una operación de gestión de Biblioteca.

## Corrección posterior: presentación del envío manual

Se confirmó que el envío de Juego de tronos terminó con 73 vídeos, con poster_path disponible, pero sin topic_presentations. El callback exigía que existiera topic_replications, excluyendo envíos manuales.

- Las series asociadas, manuales o replicadas, ejecutan SendTopicPresentation en el callback previo a copiar los vídeos. Un error detiene esa fase y sigue el tratamiento de errores/reintentos existente.
- El registro por topic evita repetir una presentación enviada. Cambiar el póster preferido en TMDB no invalida una presentación ya enviada de la misma serie; una presentación pendiente reintenta con el póster guardado y el mismo random_id.
- Una nueva creación usa el ID persistente del trabajo como identidad de deduplicación. Los reintentos conservan ese ID; una nueva importación tras borrar el topic no reutiliza el de la creación anterior.
- Se conserva el flujo separado de colecciones de películas.
- Verificación: go test ./..., go vet ./..., compilación de API y revisión del orden callback → presentación → copia. No se realizó un envío real ni se eliminó el topic del usuario.
- Referencia oficial: https://core.telegram.org/method/messages.sendMedia (foto, caption, reply_to y random_id).
- Límite: si TMDB no ofrece póster, se mantiene el comportamiento de omitir la presentación; no se inventa una imagen. Añadir presentación a un topic antiguo que carecía de ella la sitúa en el momento actual, no antes de mensajes históricos.
