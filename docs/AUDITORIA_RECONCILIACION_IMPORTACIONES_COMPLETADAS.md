# Reconciliación de importaciones completadas

Revisión: 11 de octubre de 2026.

## Causa y corrección

La recuperación comparaba los vídeos activos con el total histórico solicitado. En Ángel se habían entregado 111 mensajes y había una eliminación registrada: quedaban 110 vídeos y 110 STRM escritos. La comparación activaba repetidamente la sincronización y la generación, que reservaba los STRM como pendientes antes de escribirlos de nuevo. Esto explicaba el cambio continuo de estado en Actividad.

La recuperación ahora comprueba las identidades de las entregas completadas. Una entrega eliminada expresamente no es un hueco del catálogo; una identidad de destino que falta sí requiere recuperación. Los mensajes inactivos conservados en el catálogo también cuentan como identificados. La comprobación está limitada al topic de destino para evitar confundir mensajes de otros grupos.

Los registros antiguos sin identidades mantienen una alternativa compatible basada en cantidades, descontando las eliminaciones registradas cuando existe el registro de entregas. Sin ningún registro de entregas se conserva la comparación histórica: no puede inferirse una eliminación que nunca se registró.

No se modifican las reservas de generación, los borrados, los espejos ni la interfaz. El estado pendiente sigue siendo correcto cuando existe una escritura real.

## Validación

- Prueba de integración sobre la consulta de producción, dentro de una transacción revertida: eliminación registrada, contador histórico mayor, identidad realmente ausente y eliminación explícita de esa identidad.
- `go test ./...`, `go vet ./...` y compilación del servidor completados correctamente.
- Reinicio del servidor con el mismo directorio de datos; comprobación de salud y observación de los estados después del reinicio.
- Ángel conservó 110 exportaciones en `written`, con la misma fecha de última escritura en ambas comprobaciones posteriores al arranque. No se repitió el ciclo de regeneración observado antes. API y frontend respondieron correctamente.

## Fuentes oficiales

Telegram comunica las eliminaciones mediante el identificador del canal y los identificadores concretos de los mensajes: [updateDeleteChannelMessages](https://core.telegram.org/constructor/updateDeleteChannelMessages). Su [gestión de actualizaciones](https://core.telegram.org/api/updates) distingue los eventos y los huecos en la secuencia. La decisión de usar las identidades persistidas para la recuperación del catálogo es una aplicación de esos datos al diseño de TgStrm, no una obligación específica de Telegram sobre los STRM.
