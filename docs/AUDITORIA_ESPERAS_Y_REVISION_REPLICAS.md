# Revisión de esperas y mantenimiento de réplicas

11 de octubre de 2026.

## Hallazgos y cambios

Los trabajos visibles no eran copias detenidas: las importaciones habían terminado. Había lecturas de historial desde la revisión, la recuperación del catálogo y los topics abiertos. Cada lector reaccionaba a su propio FLOOD_WAIT sin impedir que otro repitiera una lectura durante la espera.

Además, varios planes compartían inventarios de topics. La preparación siempre empezaba un escaneo nuevo, incrementando la generación incluso cuando otro proceso acababa de obtener un inventario completo y válido. Esto propagaba revisiones desactualizadas entre planes.

- Se coordinan las lecturas de historial usadas por el inventario y la actualización de mensajes. Solo una petición de esas rutas se ejecuta a la vez. Una espera recibida se comparte; los demás lectores devuelven el tiempo restante sin consultar Telegram. La espera permanece en memoria durante esta ejecución; tras un reinicio Telegram puede comunicarla nuevamente.
- Una espera ya conocida se comprueba antes de iniciar una generación de inventario. No se invalida evidencia simplemente porque otro lector deba esperar.
- La preparación reutiliza evidencia completa de menos de cinco minutos, únicamente cuando no hay invalidación ni comprobación de eliminación pendiente. No aumenta la generación al reutilizarla. La ejecución conserva su escaneo final obligatorio antes de crear/copiar destinos.
- El fallback de archivos no identificados pasa de intentarse cada diez segundos a una vez por minuto y respeta las esperas superiores a ese intervalo. Las ediciones recibidas por eventos conservan su tratamiento inmediato.

## Seguimiento que se conserva

La búsqueda de nuevos topics sigue activa para los grupos con `discover_new_topics`, mediante `RefreshTopics`, inserción con `ON CONFLICT DO NOTHING` y paso a preparación solo cuando se han insertado nuevos elementos. Se conserva su intervalo de cinco minutos, los filtros de topics ocultos, las exclusiones explícitas y las comprobaciones de elegibilidad. Los topics nuevos sin coincidencia siguen requiriendo revisión antes de copiarse. Los cambios relevantes invalidan la evidencia y obligan a comprobarla de nuevo. No se desactiva el seguimiento ni se alteran los mensajes, las carpetas o los STRM para resolver las esperas.

Una espera temporal legítima todavía puede aparecer: no se promete eliminar los límites de Telegram ni coordinar todos los métodos de la API con este control de historial. La interfaz no ha cambiado.

## Auditoría

- Pruebas de espera compartida, reanudación y cancelación de lectores en cola.
- Pruebas PostgreSQL con transacciones revertidas sobre la consulta real de reutilización: evidencia reciente, invalidada, antigua, incompleta y eliminación pendiente.
- Regresión de invalidaciones incrementales y de recuperación de la cola de importación; respeta los cambios relevantes y las generaciones obsoletas.
- Pruebas generales de Go, análisis estático, compilación y comprobación del servidor tras reiniciar.
- No se enviaron mensajes de prueba ni se borraron datos reales durante las pruebas de base de datos.
- Tras el reinicio, API y frontend respondieron correctamente; se conservaron 614 importaciones completadas y los conteos de topics copiados de los seis planes confirmados. Los cinco planes con fuentes activas estaban comprobando evidencia pendiente del origen, sin copias en cola: no se marcaron artificialmente como completados. Telegram todavía comunicó una espera de arranque; los lectores posteriores reflejaron el plazo restante. Los 110 STRM de Ángel conservaron su fecha de escritura.
- La prueba de coordinación de lectores pasó también con `go test -race`; `go vet ./...` y la compilación terminaron sin errores.

## Corrección de comprobaciones bloqueadas en revisión

Las comprobaciones pendientes compartían el trabajador de mantenimiento con análisis, renombrados e imágenes. Además, la cola no priorizaba los topics seleccionados que bloqueaban una revisión sin confirmar, frente a más de 400 comprobaciones acumuladas.

- Se ejecuta la reconciliación de evidencia en un trabajador independiente, con cancelación y plazo de 90 segundos por ejecución.
- Se priorizan comprobaciones de topics incluidos en planes en revisión o preparación que todavía no tienen importación. Las exclusiones no bloquean la confirmación.
- Las lecturas coordinadas se separan 400 ms después de cada respuesta. Es una medida conservadora propia, no una cuota oficial de Telegram. El preflight sin petición no consume ese intervalo; FLOOD_WAIT mantiene su plazo obligatorio.
- Se conservan el bloqueo SQL, las generaciones y la comparación de evidencia: no se eliminan comprobaciones para habilitar artificialmente el botón.

Validación: `go test ./...`, `go vet ./...`, compilación y regresiones PostgreSQL de manifiestos incrementales y evidencia reciente, con transacciones revertidas, pasaron. Tras reiniciar, las comprobaciones seleccionadas del plan Series F bajaron de 43 a 25, 6, 3 y finalmente 0. La interfaz conservó 43 topics incluidos y mostró «Confirmar y replicar» habilitado. No se pulsó ese botón ni se cambiaron selecciones. Los topics excluidos pueden seguir comprobándose sin bloquear los incluidos.

## Fuentes oficiales

[Telegram: errores](https://core.telegram.org/api/errors) exige esperar el plazo indicado por FLOOD_WAIT. [Telegram: actualizaciones](https://core.telegram.org/api/updates) describe los eventos y la recuperación de huecos. La reutilización de inventarios recientes y la coordinación de estos lectores son decisiones de arquitectura de TgStrm para reducir consultas redundantes conservando esas garantías.
