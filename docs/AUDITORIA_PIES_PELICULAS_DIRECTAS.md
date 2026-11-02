# Pies de películas subidas directamente

Normalización: título confirmado en TMDB y año, por ejemplo `Willow (1988)`.

- `normalizedMovieCaption`: valida título, año y límite conservador de 1024 caracteres.
- `prepareDirectMovieCaptions`: descubre vídeos propios, activos, asociados a una película en destinos gestionados. No llama a Telegram; prepara como máximo 50 trabajos por pasada. Excluye importaciones, monitores y espejos para conservar sus flujos.
- `CorrectManagedMovieCaption`: verifica destino, identidad y pie actual en Telegram antes de editar el mismo mensaje. No cambia archivos, IDs ni medios; una edición manual concurrente produce conflicto.
- `processDirectMovieCaption`: procesa un trabajo cada cinco segundos, con bloqueo compartido con el envío de películas. Guarda el resultado después de la confirmación remota, actualiza la firma y publica un evento. La cola sobrevive a reinicios; los errores permanentes quedan en atención. FLOOD_WAIT se respeta y no se sustituye por reintentos inmediatos.
- Migración 056: añade la cola sin borrar datos. La invalidación de coincidencias reconoce exclusivamente la transición de pie registrada por la cola y con nombre de archivo intacto; las ediciones ajenas siguen invalidando coincidencias automáticas.

Auditoría: pruebas de formato/límites y errores; prueba transaccional en PostgreSQL sin llamadas a Telegram ni cambios duraderos para comprobar candidatos, exclusión de vídeos entrantes, conservación de identidad tras normalización e invalidación tras cambiar el nombre real del archivo.

Fuentes oficiales:
- https://core.telegram.org/method/messages.editMessage
- https://core.telegram.org/api/errors
- https://developer.themoviedb.org/reference/movie-details

Limitaciones: Telegram debe permitir editar el mensaje; no se modifica un vídeo de otra cuenta ni una identidad ambigua. La comprobación remota reduce conflictos, pero Telegram no ofrece edición condicional atómica por versión. No se realizó un envío de prueba a Telegram para fabricar contenido nuevo.
