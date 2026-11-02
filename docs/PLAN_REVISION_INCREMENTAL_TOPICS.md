# Revisión incremental de topics

Estado: implementación y auditoría interna completadas el 10 de octubre de 2026, después de finalizar la replicación. Validación detallada en AUDITORIA_REVISION_INCREMENTAL_PREPARADA.md.

## Implementación completada

Inventario completo conservado durante escaneos, firmas de contenido relevante, borrados conocidos selectivos, deduplicación persistente, cola de comprobación para desconocidos y coordinación transaccional con confirmación. La revisión manual permanece disponible durante las comprobaciones. Se conserva la validación final antes de enviar.

## Objetivo y límites

Conservar los análisis válidos y revisar solo el contenido afectado. Mantener las selecciones, coincidencias manuales, exclusiones, protección frente a contenido no vídeo, espejos, STRM y progreso persistente. Un borrado en origen no debe borrar las copias conservadas en destino.

No se puede garantizar conocer siempre el topic de un mensaje borrado: el evento de Telegram contiene canal e IDs, pero no el hilo. No tratar un evento desconocido como ausencia de cambios ni inventar su pertenencia.

## Hallazgos comprobados

- `realtime.go` invalida todos los manifiestos del canal antes de procesar `OnDeleteChannelMessages`.
- Para mensajes nuevos/editados invalida el topic antes de determinar si cambia contenido relevante.
- La tabla de mensajes ya permite localizar el topic de mensajes conocidos; los manifiestos guardan IDs de vídeos pero no un inventario completo de documentos examinados.
- `maintainCollections` vuelve a preparar planes cuyos manifiestos fueron invalidados. El control de revisión durante el escaneo impide publicar resultados obsoletos y debe conservarse.
- La interfaz solo permite asociación cuando todo el plan está en `review`, bloqueando también topics ajenos al cambio.

## Tareas, en orden

- [x] Inventariar todas las rutas que modifican mensajes, topics, manifiestos y planes, incluidos recuperación de conexión y análisis manual. Separar contenido de estado visual/administrativo.
- [x] Registrar la pertenencia de los mensajes relevantes examinados, incluidos documentos no vídeo y sin clasificar, sin duplicar sus archivos ni guardar contenido innecesario. Diseñar una migración aditiva compatible con la base existente.
- [x] Resolver borrados por canal e ID usando mensajes, raíz del topic e inventario del análisis, antes de desactivar registros. Invalidar exclusivamente los topics identificados; ignorar repeticiones ya procesadas.
- [x] Para eventos desconocidos, persistir una comprobación pendiente y agruparla por canal. Comparar inventarios/estado por topic antes de invalidar resultados. Una comprobación más amplia puede ser necesaria cuando no existe evidencia suficiente; no reiniciar directamente todo el análisis por cada evento.
- [x] Comparar el contenido anterior y nuevo en altas/ediciones. Revisar solo ante cambios relevantes en medios, nombre, pie o referencias de identificación. Las altas de documentos no vídeo siguen siendo relevantes para la elegibilidad estricta.
- [x] Agrupar cambios sucesivos por topic en trabajo persistente, conservando una revisión de contenido que detecte cambios durante el escaneo. No perder eventos si el proceso cae ni permitir que un resultado antiguo sobrescriba uno nuevo.
- [x] Separar el progreso de comprobación del estado de revisión del usuario. Mantener selecciones y coincidencias manuales; confirmar solo con evidencia vigente de todos los topics incluidos y sin importar resultados desactualizados.
- [x] Exponer el estado de comprobación por topic y sus errores, junto al número de topics pendientes. Permitir revisar coincidencias de topics ya disponibles sin desbloquear la confirmación de contenido pendiente. Mantener diseño y adaptación móvil actuales.
- [x] Verificar recuperación `pts`/diferencias usando la infraestructura existente, evitando una segunda conexión o mecanismos de recuperación duplicados. Conservar la detección de nuevos topics y las esperas FLOOD_WAIT.
- [x] Auditar con pruebas de firmas, comparación, inventario, borrados conocidos/desconocidos/repetidos, cola persistente y confirmación concurrente. Revisar en código las rutas conservadas de espejo, STRM y recuperación; documentar los límites. No se realizó una simulación integral de caída o borrado real de Telegram.

## Referencias oficiales

- https://core.telegram.org/constructor/updateDeleteChannelMessages — canal, IDs borrados y contadores `pts`; no incluye topic.
- https://core.telegram.org/api/threads — pertenencia al hilo mediante `reply_to_top_id`.
- https://core.telegram.org/api/updates — secuencia de eventos y recuperación de huecos.
- https://core.telegram.org/method/updates.getChannelDifference — diferencias de estado de un canal.

El diseño de inventarios, agrupación y revisiones es una propuesta para TgStrm basada en esas garantías, no una exigencia literal de Telegram.
