# Recuperación de réplica E — 2026-10-11

Diagnóstico en lectura: 118 importaciones completas (2.824 archivos), 75 pendientes, sin avance durante horas. Trabajos con cerca de 2.000 intentos mostraban «la réplica se pausó o el origen cambió». El plan seguía running. Al consultar había 63 trabajos con evidencia vigente y 12 con revisión divergente; no era una espera FLOOD_WAIT del envío.

La selección FIFO reclamaba repetidamente trabajos obsoletos que fallaban en la protección previa al envío. El mantenimiento solo seleccionaba manifiestos pendientes/invalidados, y no recuperaba aquellos ya elegibles cuya revisión divergía del trabajo.

Corrección: la cola deja fuera manifiestos obsoletos o no elegibles, respetando la misma protección de envío. El mantenimiento también selecciona revisiones divergentes de topics ya encolados y vuelve a comprobar su contenido mediante el escaneo autoritativo existente; solo después actualiza la revisión. No se elimina la validación ni se fuerzan revisiones en la base. Los registros de entrega y random_id existentes se conservan.

Verificación previa: go test ./..., go vet ./..., compilación de API y git diff --check. Consulta real de clasificación: 63 trabajos aptos frente a 12 que necesitan comprobarse. Sin cambios de interfaz, rutas, estructuras STRM o datos de selección. Las esperas de Telegram se mantienen.

Auditoría ampliada: se libera el resultado de archivos antes de consultas anidadas para preparar pies; se comprueba Scan/Rows.Err antes de enviar. Se priorizan manifiestos que retienen réplicas activas. La consulta exacta de mantenimiento pasa EXPLAIN en PostgreSQL, sin ejecutarla en esa comprobación. Pruebas y compilación pasan tras estos ajustes.

Tras arrancar, se observaron manifiestos recuperados con revisión coincidente. El plan entró en preparación por comprobaciones pendientes anteriores y registró FLOOD_WAIT (9); el envío aún permanecía en 2.824 archivos al cierre de la observación. No se afirma haber completado la réplica ni confirmado avance de entregas tras la corrección. El servicio queda ejecutándose y los trabajos anteriores se conservan.

## Cierre y prevención — 2026-10-11

La recuperación terminó la E: 193 importaciones completas y 4.785 archivos enviados. Esta cifra verifica el envío; no sustituye un recuento independiente de todos los STRM.

Se extrajeron las consultas exactas de producción para probarlas en PostgreSQL. La cola también rechaza un manifiesto ausente. La revisión de topics encolados con evidencia pendiente aparece como «Comprobando cambios», con aviso visible durante ejecución, sin modificar el diseño ni la selección.

Regresiones optativas verificadas dentro de una transacción revertida: trabajo válido, revisión obsoleta, invalidación, análisis pendiente/fallido, pausa, manifiesto ausente, reintento futuro, lease vigente y lease vencido. Un trabajo obsoleto más antiguo no impide reclamar otro válido. No se ejecutan envíos de prueba. Prueba de lectura con pool de una conexión: el resultado de archivos se libera antes de la siguiente consulta, mantiene la selección y respeta cancelación.

Validación final: go test ./..., go vet ./..., pruebas PostgreSQL citadas, npm run build, compilación de API y git diff --check. Sin migración adicional ni reinicio de registros, sin borrado o reenvío manual para reparar.

Referencias oficiales: https://core.telegram.org/api/errors (respetar FLOOD_WAIT), https://pkg.go.dev/database/sql#DB.SetMaxOpenConns y https://pkg.go.dev/database/sql#Rows.Close (pool y liberación de resultados). La política de recuperación y prioridad es diseño de TgStrm, no una garantía de ausencia de futuros límites de Telegram.

Verificación del arranque final: API /health/live devuelve ok; Vite devuelve HTTP 200. La E conserva 193 trabajos y 4.785 archivos confirmados. Tras reconectar, el plan está en preparing por comprobaciones de manifiestos pendientes; no se considera que esa comprobación haya terminado. Los trabajos completados conservan sus entregas y no se reinician desde cero.
