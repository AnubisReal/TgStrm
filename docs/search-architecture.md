# Búsqueda global

## Contratos

- `GET /api/v1/search?q={texto}&limit={1..40}` consulta el catálogo local y, si el término no está indexado, hace una sincronización dirigida de topics externos antes de consultar el índice global de Telegram. Devuelve archivos, topics y grupos ordenados por relevancia.
- `POST /api/v1/search/remote/index` indexa explícitamente un resultado remoto que pertenece a un topic de un foro conocido. Solo hidrata el topic; no reenvía ni exporta archivos.
- `GET /api/v1/tmdb/search?media_type={movie|tv}&query={texto}` conserva la búsqueda externa de candidatos TMDB. No se llama por cada pulsación.
- Las asociaciones confirmadas continúan guardándose mediante las rutas TMDB existentes del topic o archivo, que siguen siendo la única fuente de escritura.

## Flujo

1. El cliente espera 250 ms desde la última pulsación y cancela la petición anterior.
2. PostgreSQL busca nombre de archivo, pie/caption, topic, grupo, título oficial y título original.
3. Los resultados locales aparecen aunque TMDB no esté configurado o disponible.
4. Un resultado ya asociado incluye su identidad, año y póster confirmados.
5. Si no hay archivos locales (aunque sí exista el topic) y el término tiene al menos cuatro caracteres, se actualizan los metadatos de topics de los foros cuando hace falta y se hidratan únicamente los topics cuyo título coincide con el término; esto indexa también vídeos enviados como documento y sus captions sin escanear grupos completos.
6. Como último recurso se consulta `messages.searchGlobal` de Telegram. Es una lectura efímera: no inventa un grupo/topic local ni modifica el catálogo sin una acción posterior del usuario.
7. Un resultado remoto con topic permite pulsar **Preparar envío**. El servidor resuelve el grupo/topic, sincroniza su catálogo de forma idempotente y abre la revisión TMDB con el mensaje encontrado preseleccionado.
8. En la revisión se elige el tipo (`Serie` o `Película`) y el topic de destino. La búsqueda nunca genera STRM ni envía mensajes: el envío solo comienza al confirmar explícitamente la revisión.

`pg_trgm` aporta coincidencias parciales y tolerancia a errores tipográficos. Los índices son append-only mediante la migración 034.

La estrategia sigue la búsqueda de texto y trigramas documentada por
[PostgreSQL](https://www.postgresql.org/docs/17/pgtrgm.html) y usa la búsqueda
global de mensajes de [Telegram MTProto](https://core.telegram.org/method/messages.searchGlobal)
solo como respaldo.
