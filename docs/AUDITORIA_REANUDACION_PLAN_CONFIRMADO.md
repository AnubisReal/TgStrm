# Continuación de planes confirmados

14 de octubre de 2026.

El arranque invalidaba todos los manifiestos asociados a planes confirmados, incluso fuentes sin importación. El reintento manual devolvía el plan a preparación. Ambas acciones obligaban a revisar todo el grupo antes de continuar una copia ya autorizada.

- El arranque invalida los manifiestos que ya tienen una importación, conservando el control de elegibilidad para copias en cola y espejos tras una desconexión. No invalida el resto del cajón por el mero reinicio.
- Un plan confirmado en preparación vuelve a ejecución si sus grupos ya están descubiertos y sus topics incluidos sin importar conservan un estado elegible o excluido por contenido. Fuentes pendientes de análisis o revisión siguen bloqueando esa transición.
- «Revisar de nuevo» retoma la ejecución de un plan confirmado. Los borradores sin confirmar conservan la preparación normal.
- Los cambios de evidencia no fuerzan un análisis general de un plan en ejecución. Cada fuente nueva conserva el escaneo completo justo antes de crear su destino. Las importaciones ya en cola mantienen su control de revisión y reconciliación de manifiestos.
- Se conservan exclusiones, coincidencias, entregas, identificadores aleatorios estables y el manejo de FLOOD_WAIT. No se restauran manifiestos como válidos de manera artificial.

Auditoría: go test ./..., go vet ./..., compilación, git diff --check; pruebas PostgreSQL con rollback de transición confirmada, borrador sin confirmar, grupo no descubierto, topic por revisar y topic excluido. Regresiones de cola y manifiestos incrementales pasaron. El plan afectado conserva cuatro importaciones completadas, con 55 archivos enviados antes de la recuperación.

Fuentes: [actualizaciones de Telegram](https://core.telegram.org/api/updates), [errores de Telegram](https://core.telegram.org/api/errors). Se conserva la recuperación del origen y sus controles; la validación previa a cada copia evita necesitar una revisión general por el mero reinicio.

Verificación real tras el reinicio: el lease anterior caducó sin intervención manual; el plan volvió a `running`, sin error. Las importaciones completadas aumentaron de cuatro (55 archivos) a seis (81 archivos), con otra importación en ejecución. No se volvió a pulsar confirmar ni se regeneró la selección.
