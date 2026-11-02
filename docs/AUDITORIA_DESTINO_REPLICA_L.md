# Validación del destino de una réplica

14 de octubre de 2026.

La importación de «La paz de Marsella» falló con «el destino guardado no está disponible». Series L permanece activo, es un foro y pertenece al catálogo gestionado; la consulta exacta que falló devuelve ahora ese destino. El mensaje antiguo sustituyó indiscriminadamente cualquier error SQL por ese texto, por lo que no permite establecer retrospectivamente si hubo ausencia temporal, fallo SQL o cancelación. No se afirma una causa histórica que no quedó registrada.

Correcciones:

- Solo `sql.ErrNoRows` se presenta como destino no disponible. Otros errores conservan su causa mediante `%w`, para diagnóstico y clasificación de reintentos.
- Ante ausencia local en una importación en cola, se consulta una vez el grupo guardado mediante `channels.getChannels`, con el acceso de la cuenta y sin sustituirlo por otro grupo. Solo una respuesta completa del mismo foro, sin `left`, permite actualizar su estado local y continuar.
- Respuestas privadas, incompletas, de otro grupo o sin foro no autorizan reactivación. FLOOD_WAIT y errores RPC se propagan sin ocultarlos.
- Los deadlines agotados siguen el reintento espaciado existente de errores temporales, conservando importación, progreso y registro de entregas.

Auditoría: pruebas generales de Go, go vet, compilación y git diff --check. Pruebas de aceptación de foro exacto y rechazo de otro ID, `left`, `min`, no-foro y `channelForbidden`. Regresiones PostgreSQL de reintento con progreso conservado, recuperación de cola y reanudación de planes confirmados, con rollback. No se borraron topics, mensajes ni STRM.

Se reinició el servicio y se reactivaron únicamente el plan L confirmado y su importación fallida, mediante la transición de recuperación existente. Los 197 topics completados y 4.839 archivos previos se conservaron.

Fuentes: [channels.getChannels](https://core.telegram.org/method/channels.getChannels), [constructor channel](https://core.telegram.org/constructor/channel), [errores](https://core.telegram.org/api/errors).

Verificación posterior: el plan volvió a `running` sin error y avanzó a 199 importaciones completadas, con 4.858 archivos, además de una importación en ejecución. «La paz de Marsella» quedó pendiente sin el error anterior; no se afirma que ese topic concreto haya terminado todavía.
