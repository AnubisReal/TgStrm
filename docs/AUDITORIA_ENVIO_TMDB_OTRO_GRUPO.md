# Envío con asociación TMDB a otro grupo

Fecha: 2026-10-25.

## Comportamiento
«Asociar y enviar serie» abre la revisión existente. Cada operación nueva muestra Biblioteca como destino principal; la propuesta por letra usa únicamente los grupos de ese catálogo. «Otro grupo» permite buscar un foro de la misma cuenta, crear/reutilizar el topic de la ficha TMDB o seleccionar uno existente. La elección solo vive en esa revisión y no escribe los ajustes de catálogo.

Se conserva el pipeline de importación: selección real de vídeos, nombre normalizado del topic, presentación con póster/enlace antes de copiar, pies de episodio normalizados, entrega persistente y reintentos con identidades estables. «Copiar sin asociar» permanece como acción distinta para una copia directa con pies originales.

## Arquitectura y protecciones
- El resolver valida cuenta, grupo activo con foro, pertenencia del topic, origen distinto e identidad TMDB compatible. Telegram valida los permisos efectivos; no se cambia el modo del grupo ni se eluden restricciones.
- Migration 062 añade catalogue_enabled (true para los trabajos anteriores). La pertenencia al catálogo se fija dentro de la transacción al guardar el destino.
- Los trabajos externos no amplían managed_catalog_chats. Se omite la generación automática de STRM al terminar esos trabajos; las réplicas y los envíos al catálogo conservan su comportamiento.
- El worker recupera el destino exacto guardado desde chats y la cuenta, sin depender de que forme parte de Biblioteca. La presentación admite ese mismo destino validado.
- Los topics de un destino externo se cargan bajo demanda; el selector conserva PillSelect, búsqueda y posibilidad de crear uno nuevo.
- La elección temporal se limpia al iniciar otra operación; Biblioteca vuelve a ser el valor inicial.

## Errores adicionales encontrados en el recorrido
- La redirección al historial de un grupo sin topics se ejecutaba también en la revisión TMDB. Ahora se excluye esa ruta para que se pueda asociar desde el historial directo.
- The Blacklist (2013-2023) conservaba el rango en la consulta. Se añade el reconocimiento de rangos entre paréntesis/corchetes y se usa el primer año, manteniendo los formatos previos.
- La publicación de progreso tenía parámetros sin tipo dentro de jsonb_build_object. Se explicitan los casts text para que PostgreSQL pueda resolverlos.

## Auditoría
- go test ./..., go vet ./... y compilación de API: correctos.
- npm run build: correcto; aviso previo de bundle >500 kB.
- Pruebas de integración con base real: destinos manuales anteriores, comparación de selección pendiente y nuevo destino externo. La prueba de la cola externa usa rollback: comprueba que catalogue_enabled=false no incorpora el grupo a managed_catalog_chats y que el comportamiento heredado de réplica permanece.
- Pruebas de rangos de año añadidas a la batería existente.
- Navegador autenticado: The Blacklist, 218 vídeos. Biblioteca propone Series T. Se eligió Otro grupo → Zone, se cargaron sus topics y permaneció disponible la creación del topic TMDB. Al abrir otra operación volvió Biblioteca → Series T y se encontró The Blacklist automáticamente con el título original que incluye el rango de años.
- No se confirmó ningún envío, no se crearon topics remotos y no se cambiaron ajustes del usuario. La comprobación visual cubre la revisión y los selectores; la entrega real se realizará cuando el usuario confirme.

## Fuentes oficiales
- https://core.telegram.org/api/forum
- https://core.telegram.org/method/messages.sendMedia
- https://core.telegram.org/method/messages.forwardMessages
