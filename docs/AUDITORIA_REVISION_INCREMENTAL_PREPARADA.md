# Auditoría del bloque preparado de revisión incremental

Fecha: 2026-10-10. Estado histórico: primer bloque preparado; completado y auditado posteriormente. Véase el cierre al final.

## Alcance revisado

Resolución de borrados, catálogo de mensajes, inventario de manifiestos, inicio/finalización de escaneos, confirmación y ejecución de planes, mantenimiento de colecciones, espejos y recuperación de eventos. Se revisó código y se consultó la base en lectura. No se ejecutaron borrados, reenvíos ni invalidaciones contra datos reales.

## Propiedades conservadas por el bloque

- Resolución por cuenta y canal, evitando colisiones de IDs entre grupos.
- Compatibilidad del JSON de evidencia: los contadores y referencias anteriores permanecen; el inventario se añade sin cambiar la elegibilidad.
- ZIP, RAR y documentos desconocidos siguen contando para la protección de contenido.
- Se mantienen los manejadores existentes de eliminación del catálogo y eventos de interfaz; el cambio preparado afecta a la invalidación previa, no a los STRM ni a los mensajes destino.
- No se cambia la conservación de copias cuando desaparece el origen ni la infraestructura existente de recuperación `pts`/diferencias.
- La comprobación de revisión al finalizar el escaneo y las validaciones del endpoint de confirmación permanecen.

## Hallazgos que impiden considerar completo el plan

1. **Inventario durante un escaneo.** `ScanPersistedTopicEligibility` inicia mediante `SaveTopicManifest` con manifiesto vacío. Este reemplaza evidencia e IDs anteriores antes de completar el nuevo escaneo. Un borrado de documento no catalogado durante ese intervalo pierde su pertenencia conocida y cae al respaldo global. Conservar el último inventario verificado mientras se construye una nueva generación, sin autorizar confirmación con evidencia pendiente.

2. **Borrados desconocidos.** El bloque preparado aún usa invalidación global si falta un ID del lote. Hace falta reconciliación persistente, agrupada y selectiva. No ignorar desconocidos ni considerarlos inocuos.

3. **Repeticiones entre eventos.** La resolución deduplica dentro de un lote, pero avisos repetidos vuelven a incrementar revisiones e invalidar. Conservar evidencia de eventos procesados o estado equivalente, compatible con la recuperación de Telegram y las caídas.

4. **Confirmación concurrente.** La invalidación de manifiestos y cambio del plan son dos escrituras separadas. La confirmación bloquea el plan, pero la primera escritura de invalidación no comparte ese bloqueo. Revisar conjuntamente la transacción y el orden de bloqueos, sin invertirlo entre consumidores. La ejecución ya vuelve a escanear antes de crear destinos; conservar esa protección hasta demostrar que una comprobación incremental puede reemplazarla.

5. **Documento nuevo/editado.** La vía actual invalida antes de comparar el contenido relevante; no basta con mejorar borrados. Comparar medios, pie, nombre y referencias sin eliminar el efecto de un ZIP o documento desconocido sobre elegibilidad.

6. **Estado del plan e interfaz.** Un solo topic invalidado sigue poniendo el plan en `preparing` y oculta todos los botones de asociación. Separar progreso por topic de revisión manual y mantener la confirmación bloqueada solo cuando los incluidos carezcan de evidencia válida.

7. **Coste del inventario.** El lookup por catálogo usa pertenencia existente, pero la rama de manifiestos consulta arrays JSON para cada ID eliminado. Evaluar cardinalidad y plan de ejecución para lotes grandes antes de desplegar; no crear índices o migraciones en el servicio activo durante esta preparación.

## Validación realizada y límites

Pasaron las pruebas del primer bloque, incluyendo resolución SQL real de solo lectura, lotes de varios topics, documentos no vídeo y compatibilidad de contadores. Pasaron `go test ./...`, `go vet ./...` y compilación separada. Estas pruebas no cubren aún las correcciones de concurrencia, reconciliación de desconocidos o recuperación tras caída: no se afirma que estén resueltas.

Durante esta auditoría la API seguía siendo el proceso PID 935; el plan activo tenía topics `queued` y `completed`. No se aplicó el binario preparado.

Referencias: https://core.telegram.org/constructor/updateDeleteChannelMessages y https://core.telegram.org/api/updates. La estrategia de inventarios y transacciones es diseño de TgStrm, no una obligación literal de Telegram.

## Cierre de la auditoría — 2026-10-10

Los hallazgos 1–6 se corrigieron antes de sustituir el servicio:

- El inicio del escaneo conserva el inventario anterior; el resultado nuevo mantiene la protección por generación.
- Los borrados conocidos invalidan exclusivamente su topic. Los desconocidos se agrupan en una cola persistente con espera, reintentos y comprobación antes de invalidar. Un inventario completo permite descartar mensajes ajenos.
- Un registro persistente evita procesar otra vez el mismo borrado. Las firmas relevantes comparan documento, nombre, pie y referencias; ignoran metadatos de transporte y visualización.
- Invalidación y confirmación comparten el bloqueo del plan dentro de transacciones, con orden estable. Se mantiene el escaneo final antes de crear destinos.
- La interfaz permite ajustar coincidencias y selección durante la preparación, manteniendo bloqueada la confirmación mientras los incluidos requieren comprobarse.
- La migración 060 añade tablas e índice de vencimiento; no elimina ni reemplaza información existente. No se añaden copias de archivos.

Validación: `go test ./...`, `go vet ./...`, compilación de API y construcción del frontend satisfactorias. La regresión optativa de PostgreSQL verifica migración, inventario conservado, deduplicación, firmas, cola con recuperación por vencimiento y bloqueo concurrente de confirmación. Todos sus cambios se revierten al terminar.

Límites: un manifiesto antiguo sin inventario completo puede requerir comprobar varios topics. Esto no equivale a invalidarlos ni a repetir sus coincidencias. La búsqueda en arrays JSON sigue sin un índice específico: no se afirma haber realizado una prueba de carga masiva. No se ejecutaron reenvíos ni borrados reales como prueba; las rutas existentes de espejos, STRM y recuperación de Telegram se conservaron. La compilación del frontend no sustituye una auditoría visual en todos los dispositivos.

## Verificación después del arranque

La API responde `status: ok` y el frontend devuelve HTTP 200. La migración 060 está aplicada; el conteo de exportaciones permanece en 15.779 y la cola nueva de borrados desconocidos está vacía. La recuperación existente reanudó cuatro planes en preparación; había 435 manifiestos invalidados pendientes. No se reinició su contenido ni se borraron copias como parte de esta auditoría. Telegram devolvió esperas FLOOD_WAIT al recuperar actividad; se mantienen los mecanismos de espera existentes.
