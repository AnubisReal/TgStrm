# Recuperación de errores temporales de Telegram

14 de octubre de 2026.

El plan indicado por el usuario pasó a atención porque «Las sirenas de Mako» recibió `500 POSTPONED_TIMEOUT` al preparar el destino. Había cuatro topics completados. La importación clasificaba como definitivos todos los errores salvo FLOOD_WAIT, cancelación, pausa y reconexión.

Los errores RPC 5xx ahora vuelven a pendiente con espera entre 30 y 300 segundos según los intentos acumulados. Se mantiene la misma importación, los identificadores aleatorios estables y el registro de entregas; no se reinicia su conteo. La actualización exige el lease vigente, publica progreso y libera el trabajo. Los errores permanentes conservan su estado fallido y recuperación manual.

La revisión incluye estos reintentos en el indicador de espera existente. Si una importación falla definitivamente, el mensaje del plan identifica el topic, conserva el error y explica la opción «Revisar de nuevo». Esa opción ya reactivaba las importaciones fallidas sin cambiar las completadas.

Validación: pruebas generales de Go, go vet, compilación y regresiones PostgreSQL de recuperación de cola. Una prueba SQL con rollback verificó conservación de completed_count, demora máxima de 300 segundos, liberación del lease y rechazo de un trabajador antiguo. No se borraron mensajes, STRM ni topics. Se reinició la API y se reactivó únicamente el plan confirmado y su importación fallida con la misma transición de recuperación de la interfaz.

Referencia: [Telegram: errores](https://core.telegram.org/api/errors) describe 500 como fallo interno del servidor y exige respetar FLOOD_WAIT. El backoff es una decisión del cliente; no garantiza que Telegram no vuelva a fallar.
