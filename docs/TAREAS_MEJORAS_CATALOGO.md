# Mejoras funcionales del catálogo

- [x] 1. Perfil Jellyfin compartido: películas con varias versiones, prefijo idéntico a la carpeta y nombres estables sin sobrescribir archivos distintos.
- [x] 2. Selector de temporadas en vista normal y gestión, compatible con paginación y eventos en tiempo real.
- [x] 3. Motivo de revisión visible: distinguir coincidencia pendiente, conflicto y error de exportación, sin abrir TMDB para errores que no son de identificación.
- [x] 4. Archivos con varios episodios: detección de rangos, huecos correctos, nombres STRM y preservación del rango al importar.
- [x] 5. Auditoría final de backend, frontend, compatibilidad y diseño adaptable.

Previsualización masiva aplazada: no aporta suficiente valor ahora frente a la revisión existente. NFO excluido por petición del usuario.

Fuentes oficiales:
- https://jellyfin.org/docs/general/server/media/movies/ — versiones múltiples y nombres con el mismo prefijo que la carpeta.
- https://jellyfin.org/docs/general/server/media/shows/ — archivos con varios episodios.

Las comprobaciones no deben reenviar, borrar contenido de Telegram ni regenerar masivamente la biblioteca real.

Auditoría realizada:
- `go test ./...`, compilación de frontend y `git diff --check` correctos.
- Selector probado con una serie real: temporada 2, ocho archivos ordenados; móvil de 390 px sin desbordamiento horizontal.
- Diálogo de duplicado comprobado en escritorio y móvil. Se corrigió su alineación y conserva navegación por teclado y cierre con Escape.
- Pruebas de nombres Jellyfin, versiones distintas sin colisión, rangos válidos e inválidos y metadatos de episodios.
- Se comprueban los errores de lectura de PostgreSQL antes de devolver faltantes: una lectura incompleta no debe producir falsos huecos.

Compatibilidad:
- No se cambia el esquema ni se renombra masivamente la biblioteca existente. Los archivos existentes adoptan el formato elegido al regenerarlos.
- Jellyfin utiliza identificadores persistentes para distinguir versiones de películas; no se inventan etiquetas de calidad.
- La sustitución existente de capítulos individuales permanece. Los rangos continuos reconocidos usan `S01E01-E02` y cuentan todos sus episodios como presentes.
- No se han reenviado ni eliminado vídeos de Telegram para estas comprobaciones. La agrupación en un servidor Jellyfin real queda pendiente de una prueba de integración.

Ajuste solicitado de nombres y selector:
- ID TMDB opcional mediante interruptor persistente en ajustes, desactivado por defecto y compatible con configuraciones anteriores. Afecta películas y series del perfil Jellyfin.
- Sin ID: carpeta `Título (Año)`. Se conserva el prefijo exacto para versiones de películas. No se renombra la biblioteca existente automáticamente.
- Selector de temporadas bajo Vídeos; comprobado en escritorio y móvil de 390 px sin desbordamiento. Separación de gestión ajustada a la nueva altura.
- Pruebas Go y compilación de frontend correctas; pruebas específicas con ID activado y desactivado.

Selector de temporadas personalizado: pildora y menú con colores neutros, tema oscuro, selección marcada, navegación por teclado y cierre con Escape o al tocar fuera. Compilación correcta; probado cambio a temporada 2 y cierre, móvil 390 px sin desbordamiento. No se modifica el filtrado ni el backend.

Revisión del selector tras feedback: se reemplaza el menú de acciones por combobox/listbox siguiendo WAI-ARIA APG. El foco permanece en el selector, Escape conserva selección, Enter confirma y Tab cierra. Una superficie continua une control y opciones. Probado con serie real: temporada 2 muestra ocho archivos; selección móvil de temporada 1, sin desbordamiento; cierre exterior y teclado correctos. Compilación y diff correctos. Servicios de desarrollo iniciados usando data existente.

Selector definitivo con Headless UI Listbox, sustituyendo la gestión artesanal de foco y teclado. Conserva superficie compartida y posición. Prueba real: temporada 2, ocho archivos; móvil 390 px sin desbordamiento y Escape correcto. Compilación correcta, npm audit sin vulnerabilidades y diff limpio. La dependencia aumenta el paquete; Vite emite aviso de tamaño (no error de compilación).
