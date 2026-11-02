# Carga de topics al abrir una revisión

12 de octubre de 2026.

## Problema confirmado

El borrador se crea antes de consultar los topics de Telegram. Durante ese intervalo la vista mostraba `0/0` y «No hay topics con estos filtros», aunque el grupo aún no había sido consultado. El trabajador de planes ejecutaba después de cada paso mantenimiento, descubrimiento de nuevos topics, renombrados e imágenes, con plazos de hasta 90 segundos por tarea. Eso retrasaba tanto el descubrimiento inicial como cada paso posterior del análisis.

El plan de Series G contenía 49 topics cuando se consultó la base de datos; la interfaz mostró esos mismos 49 al abrirse. No era una pérdida de contenido, sino una carga diferida mal comunicada y una cola que mezclaba tareas.

## Corrección

- Preparación y ejecución de planes tienen un trabajador separado del mantenimiento de colecciones, renombrados e imágenes.
- Ambos conservan sus límites de contexto, cancelación, arrendamientos de trabajos, revisiones y fechas de reintento. No se aumenta el paralelismo de lecturas de historial: siguen usando el control compartido existente.
- El grupo en estado `pending` muestra «Cargando topics del origen…», sin presentar un 0/0 como catálogo terminado.
- Mientras todavía no se han obtenido topics, la vista informa de que aparecerán automáticamente. El mensaje de filtro vacío se conserva para resultados ya cargados.
- No se inicia ninguna copia, ni se cambia la selección o las coincidencias para solucionar la carga.

## Auditoría

Pasaron `go test ./...`, `go vet ./...`, compilación de la API, `npm run build` y `git diff --check`. Pasaron también las regresiones PostgreSQL optativas de recuperación de cola, rechazo de duplicados antes de confirmar, invalidación incremental y reutilización de evidencia reciente; sus transacciones se revierten. Se revisó que el trabajador nuevo no duplica el registro del anterior ni sus llamadas de mantenimiento.

Se reinició la API con el mismo directorio de datos. El subdominio del frontend continúa autorizado solo mediante su variable de entorno. No se alteraron rutas de STRM ni configuración de despliegue.

## Referencia

[Telegram: manejo de errores](https://core.telegram.org/api/errors) exige respetar el plazo de FLOOD_WAIT. La separación de trabajadores es una decisión interna para evitar bloqueos entre tareas, y mantiene esas esperas; no promete eliminar los límites de Telegram.
