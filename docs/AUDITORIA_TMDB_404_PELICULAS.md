# Auditoría de respuestas 404 en películas

Fecha: 2026-10-09.

TMDB devolvía candidatos en `search/movie` cuyos endpoints `alternative_titles` respondían 404. El análisis abortaba y la cola repetía indefinidamente el mismo archivo. Se reprodujo con consultas de solo lectura: candidatos 1757995, 1778285 y 1702857.

La identificación ahora descarta y recuerda candidatos no disponibles al consultar títulos alternativos, créditos o fechas regionales. Continúa evaluando los demás con las mismas reglas de título, año, ambigüedad y límites. Los errores de transporte, 429 y 5xx siguen propagándose para reintento; no se interpretan como ausencia de coincidencia.

Un 404 durante la identificación o la consulta de la ficha final pasa a revisión, sin reintentos indefinidos. Se conserva el error tipado al consultar la ficha y al envolver errores de búsqueda. No se cambia el manejo de errores de envío, indexación o generación STRM.

Validación: pruebas con candidato inexistente seguido de alias válido y con único candidato inexistente; pruebas completas `go test ./...`, `go vet ./...`, compilación y `git diff --check`. Auditoría real optativa de solo lectura en `TestMovie404ReadOnlyAudit`.

Tras reinicio controlado de la API, los dos archivos identificables pasaron a `ready` sin error y el restante a `review` con el motivo de coincidencia no única. La API responde 200. No se modificaron manualmente los registros de la cola ni los archivos generados.

Referencia oficial: https://developer.themoviedb.org/docs/errors (404: identificador o recurso no encontrado).
