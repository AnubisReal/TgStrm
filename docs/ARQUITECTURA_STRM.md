# Arquitectura STRM de TgStrm

## Decisiones estables

1. Cada vídeo obtiene un UUID público inmutable en `stream_assets`. Los `.strm` contienen `/stream/{uuid}` y nunca IDs de sesión, `file_reference` ni credenciales MTProto.
2. La ubicación Telegram es mutable. TgStrm resuelve el mensaje/documento vigente y renueva referencias caducadas sin cambiar la URL pública.
3. Películas y series usan destinos separados. Las rutas y nombres se construyen con datos confirmados de TMDB y se registran antes de tocar disco.
4. La escritura será idempotente y atómica: archivo temporal en el mismo directorio, sincronización, renombrado y registro del resultado.
5. Una eliminación de Telegram no borrará inmediatamente archivos de biblioteca. Se marcará como ausente y se aplicará la política configurada, con historial auditable.
6. La sincronización automática consume el outbox existente. Los eventos pueden repetirse; una restricción `(library_id, asset_id)` garantiza idempotencia.
7. La credencial de TMDB (API Key v3 o Read Access Token v4) se cifra en reposo, solo se envía desde el formulario al servidor y nunca se devuelve a la web.
8. La raíz persistente de la aplicación es `data` (`/data` en contenedor) y contiene configuración cifrada, caché y copias. Las bibliotecas STRM son destinos independientes configurables; pueden usar rutas externas como `/media/movies` y `/media/shows` montadas desde otros discos. Los valores `data/library/*` son únicamente una alternativa local predeterminada.
9. La navegación superior contiene áreas del producto; la barra lateral contiene herramientas contextuales. La lectura normal permanece limpia y la gestión de un grupo usa `/groups/{id}/manage`.
10. El streaming entrega primero 256 KiB y después prelee hasta tres partes MTProto de forma concurrente, conservando orden, cancelación y el máximo oficial de 1 MiB por llamada.
11. El modo lateral de trabajo es independiente del nivel del catálogo. Cambiar entre vista normal y gestión sustituye la entrada actual del historial; al volver a grupos se conserva el modo elegido.
12. La caché multimedia persistente vive bajo `data/cache`, tiene un límite global y expulsión LRU. Solo conserva hasta el primer MiB y los bloques del índice MP4 detectado; el cuerpo reproducido es temporal. Usa la identidad inmutable del documento, escritura atómica y validación de tamaño, sin alterar el vídeo ni la URL STRM.
13. Cada respuesta HTTP `206` entrega como máximo 8 MiB aunque el cliente solicite desde un punto hasta el final. El `Content-Range` describe exactamente el subconjunto enviado y el reproductor pide el siguiente tramo; así, un salto abandonado no retiene durante minutos la cola de descargas MTProto.
14. Las descargas intentan usar un pool MTProto persistente de cuatro conexiones dedicado al DC del archivo. Se prueba primero `media_only` y después un pool de datos aislado en el mismo DC. Si Telegram rechaza la transferencia de autorización para ese DC, se reutiliza el enrutamiento compatible de gotd y se impide otro intento durante diez minutos para evitar `FLOOD_WAIT`. Tres conexiones atienden la prelectura y queda capacidad para priorizar un salto nuevo cuando el pool está disponible.

## Flujo de producto

1. Ajustes: configurar URL pública, rutas de películas/series y TMDB; probar cada dependencia por separado.
2. Grupo: modo selección con seleccionar visibles, seleccionar todos, limpiar y selección individual. Buscar no debe perder la selección.
3. Topic: acción para preparar/generar ese topic y selección opcional de vídeos concretos.
4. Revisión: previsualizar coincidencias TMDB, estructura final, conflictos y elementos omitidos antes de escribir.
5. Trabajo: crear una ejecución persistente, procesarla en segundo plano y mostrar progreso/errores por WebSocket.
6. Automatización: habilitarla por regla de topic solo después de una generación manual correcta.
7. Reconciliación: comparar PostgreSQL con disco, reparar faltantes y detectar archivos ajenos sin eliminarlos.

## Orden de implementación

- [x] Identidad pública permanente y endpoint de streaming.
- [x] Esquema de destinos, reglas y exportaciones.
- [x] API y vista de configuración de medios; credencial TMDB cifrada y nunca devuelta al navegador.
- [x] Destinos automáticos bajo la raíz persistente y comprobación real de la API Key v3 o Read Access Token v4 con TMDB.
- [x] Selección de topics en el grupo: individual, todos los visibles y ninguno, conservada al buscar.
- [x] Estadísticas de vídeos y estado STRM por topic.
- [ ] Selección de vídeos dentro del topic.
- [x] Búsqueda oficial en TMDB, revisión manual y persistencia de coincidencias confirmadas.
- [ ] Planificador de estructura de películas y series.
- [x] Primera escritura atómica e idempotente de STRM individuales, por topic y por selección de topics.
- [ ] Convertir la ejecución síncrona actual en trabajos persistentes con progreso y reintentos.
- [ ] Sincronización en tiempo real y reconciliación.
- [x] Caché multimedia persistente y limitada por bloques, con métricas, configuración y limpieza desde Ajustes.
- [x] Detección estructural y precarga completa de índices MP4 tardíos (`moov`) que ocupen varios bloques, limitada a 32 MiB y tres trabajadores.
- [x] Rangos HTTP parciales acotados a 8 MiB para que los saltos cancelados liberen rápidamente la descarga y el reproductor pueda continuar desde la nueva posición.
- [x] Pool MTProto dedicado por DC multimedia, calentado al conectar, con cuatro conexiones, transferencia de autorización y alternativa segura al endpoint normal del mismo DC.
- [ ] Contenedores, volúmenes, permisos y pruebas cruzadas.
