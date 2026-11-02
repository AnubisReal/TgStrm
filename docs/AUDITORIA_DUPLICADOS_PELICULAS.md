# Protección de duplicados en envíos monitorizados de películas

Identidad: documento de Telegram + grupo destino + topic destino. El título, el año y el pie no se usan para decidir si dos archivos son iguales. Documentos distintos se conservan como versiones, aunque compartan título o tamaño.

- La migración 057 añade un registro de propiedad persistente con clave primaria compuesta y restricción única por trabajo. Inicializa un propietario por cada archivo ya indexado; no elimina ninguna copia anterior.
- `reserveMovieDeliveryFile` comprueba el catálogo antes de iniciar la presentación. Reserva la identidad atómicamente en PostgreSQL. Otro mensaje de origen con el mismo documento no puede crear una segunda reserva para ese destino.
- `guardMovieDeliveryFile` se ejecuta antes del póster y nuevamente antes del vídeo. Un archivo presente se omite con una razón guardada. Si solo hay una reserva pendiente, el segundo trabajo espera en lugar de perderse. Una identidad de origen o destino cambiada durante el trabajo requiere revisión.
- El propietario conserva su reserva durante reinicios, errores y respuestas inciertas. Sus random_id de póster y vídeo permanecen intactos y se reutilizan en la reconciliación existente.
- Una nueva reserva puede sustituir a una copia completada únicamente si el catálogo ya confirma que su mensaje destino está eliminado. No se libera un envío incierto por tiempo transcurrido.

Pruebas transaccionales (sin Telegram ni escrituras STRM): duplicado con otro mensaje origen; reintento del mismo propietario; etapa de envío incierto; cambio del documento o destino tras reservar; documento distinto con mismo nombre; archivo subido directamente al destino; copia completada eliminada; inicialización de propietarios históricos. Toda la prueba se revierte.

Fuentes oficiales:
- https://core.telegram.org/method/messages.sendMedia (random_id evita reenviar el mismo mensaje).
- https://www.postgresql.org/docs/current/indexes-unique.html (unicidad compuesta garantizada por la base de datos).

Alcance y límites: evita duplicar la misma identidad Telegram en el flujo monitorizado de películas. No identifica por contenido dos subidas independientes con IDs distintos; requeriría comprobar los bytes. No impide que alguien suba manualmente el mismo archivo dos veces en Telegram. No borra las 66 copias adicionales encontradas previamente. No modifica series, espejos de series ni el formato de STRM.

## Limpieza de copias existentes (2026-10-09)

La limpieza autorizada se registra explícitamente en `movie_duplicate_cleanup`;
la migración 058 crea una cola vacía y nunca inicia borrados por sí sola.
El plan local contiene 66 entregas redundantes de Películas. Conserva el
propietario de `movie_delivery_files` y comprueba directamente en Telegram que
su vídeo sigue existiendo y tiene el mismo documento. Solo elimina el vídeo
redundante y su mensaje de foto con el enlace TMDB exacto registrado.
Versiones con otro documento no se consideran duplicadas.

Cada par se verifica antes de llamar a `channels.deleteMessages`. Una identidad
incompatible pasa a revisión; un fallo temporal conserva el trabajo pendiente.
Los FLOOD_WAIT aplazan la cola completa. El bloqueo asesor compartido evita
solapamientos con los envíos. Un reinicio después del borrado remoto puede
repetir la comprobación: los mensajes redundantes ya ausentes se aceptan,
pero el superviviente debe seguir presente.

Los mensajes locales se desactivan con el flujo existente de eventos. El
reconciliador retira sus STRM y registros de exportación, conservando las
carpetas y versiones supervivientes. Las entregas eliminadas quedan excluidas
para que el escaneo no vuelva a copiarlas, y la cola conserva el resultado
como registro de auditoría.

Validación: pruebas completas de Go y go vet; pruebas específicas de documento
distinto, superviviente ausente, póster ajeno, enlace TMDB parecido pero
incorrecto, repetición tras borrado y protección del superviviente.

Fuentes oficiales: https://core.telegram.org/method/channels.getMessages y
https://core.telegram.org/method/channels.deleteMessages.

Resultado de la ejecución local: 66 trabajos completados sin errores (66 vídeos
redundantes y sus 66 presentaciones), 66 mensajes de vídeo desactivados, cero
grupos de documentos duplicados activos en Películas y cero exportaciones
STRM asociadas a las copias eliminadas. Se conservan los 3473 documentos
supervivientes. El servicio queda ejecutándose con la prevención y limpieza
persistentes activas; no se modificaron las series ni se publicó en GitHub.
