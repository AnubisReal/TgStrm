# Modo ambiente

- [x] Ajustes persistentes: activación, espera de 1 a 120 minutos y tendencias del día o semana. Desactivado inicialmente.
- [x] Endpoint autenticado `GET /api/v1/ambient/trending`, con protección de origen, credencial TMDB en Go y caché en memoria de una hora.
- [x] Tendencias de películas y series en español; exclusión de personas, contenido adulto y entradas sin imagen horizontal.
- [x] Módulo React aislado: pausa con pestaña oculta, formularios, diálogos o reproducción; cancelación de solicitudes y espera ante errores.
- [x] Presentación adaptable, cambio cada 12 segundos, precarga de la próxima imagen y respeto de movimiento reducido.
- [x] Controles dentro del salvapantallas para pantalla completa y volver a la vista anterior. El control de pantalla completa conserva el modo ambiente.
- [x] Botón de prueba en ajustes; funciona sin activar ni guardar el modo automático.
- [x] Pruebas Go y compilación del frontend. Revisión visual con tendencias reales en escritorio y ancho móvil de 390 px.

## Auditoría

La configuración amplía el JSON existente sin migraciones de esquema. No modifica el catálogo, STRM, mensajes, espejos ni sesiones de Telegram. Las consultas reutilizan el cliente TMDB y su limitador. La caché no contiene credenciales y mantiene únicamente un periodo de tendencias. El contenido se presenta como texto de React, sin HTML remoto. La ruta utiliza la autenticación administrativa existente.

Se verificaron respuesta de caché, rechazo de periodos inválidos, rechazo de origen externo y comprobación con detector de carreras mediante `go test -race`. `go test ./...` y `npm run build` completaron correctamente. Vite conserva el aviso de paquete superior a 500 kB.

La emulación móvil comprueba distribución, no sustituye una prueba en Safari/iOS real. El botón de pantalla completa se oculta cuando la API no está disponible; el navegador puede rechazar la petición. No se impide la suspensión del sistema operativo ni se altera la caducidad de la sesión.

## Fuentes oficiales

- [Tendencias TMDB](https://developer.themoviedb.org/reference/trending-all)
- [Imágenes TMDB](https://developer.themoviedb.org/docs/image-basics)
- [Fullscreen API](https://developer.mozilla.org/en-US/docs/Web/API/Element/requestFullscreen)
- [Dialog de Headless UI](https://headlessui.com/react/dialog)

Activación automática comprobada con un minuto de espera en Biblioteca; regreso a la vista anterior correcto. Ajustes de prueba restaurados a desactivado y cinco minutos.

Entrada y salida del modo de pantalla completa comprobadas mediante ambos botones, manteniendo abierto el salvapantallas. `go vet ./internal/httpapi` correcto.

## Transición suave y compatibilidad gráfica

- Dos elementos de imagen estables; la imagen actual permanece opaca durante el fundido de la siguiente, evitando revelar el panel inferior entre cambios.
- Se espera la descarga y `HTMLImageElement.decode()` antes de presentar la siguiente imagen. Navegadores sin `decode()` esperan `load`.
- Fundido de opacidad de 1,4 segundos, sin escala ni transformaciones. Contenedor oscuro permanente y capas aisladas.
- Solicitudes y callbacks de imagen invalidados al cerrar o cambiar de candidato; frames y temporizadores limpiados. Los errores conservan el comportamiento de saltar candidatos fallidos.
- Movimiento reducido sin transición. No cambia API, base de datos, configuración ni catálogo.
- Compilación del frontend y revisión de diff correctas. Comprobados cambios con imágenes TMDB reales; confirmadas imágenes de 1280 px decodificadas, dos capas y ausencia de transformaciones.
- Falta confirmar en el Portal de Meta con Brave real: la revisión local no demuestra que un defecto específico de su compositor haya desaparecido.

Referencia: [decode() y sustitución de imágenes](https://developer.mozilla.org/en-US/docs/Web/API/HTMLImageElement/decode).

## Corrección de montaje en iOS y sincronización visual

La captura del usuario identifica una llamada a `document.exitFullscreen()` inexistente en Safari/iOS. La comparación anterior podía considerar que una referencia vacía era el elemento fullscreen al coincidir con `fullscreenElement=null`. Se comprueba ahora que el panel existe y que los métodos están disponibles; se sale únicamente del fullscreen que pertenece al panel. No se modifica el sistema de sesiones.

Cuatro pruebas de regresión cubren navegador sin API, referencia vacía, fullscreen ajeno, API parcial y salida del panel propio: `node --test web/tests/ambient-fullscreen.test.mjs`. Resultado correcto; compilación TypeScript/Vite y diff correctos.

Imagen, degradado, título y sinopsis comparten una misma capa de transición. Se eliminan los textos auxiliares del salvapantallas; se conserva la atribución TMDB en ajustes. Botones con fondo apenas translúcido y sin filtro de desenfoque para ver el póster detrás. Comprobados candidatos reales con imagen y texto en cada capa, controles y presentación de escritorio. Safari/iOS y Portal reales siguen requiriendo verificación por el usuario.
