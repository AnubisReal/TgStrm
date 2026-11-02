# Gestión y Biblioteca — tareas y auditoría

Alcance: conservar las vistas, píldoras y catálogo existentes. Sin migraciones ni cambios en los enlaces STRM. Auditar cada tarea antes de cerrarla.

- [x] 1. Biblioteca: acceso al grupo principal con la vista existente; acceso móvil y estado sin grupo configurado.
- [x] 2. Filtros de gestión: Todos, Por revisar y Sin STRM; búsqueda combinable, sin afectar la vista normal.
- [x] 3. Selección explícita: seleccionar visibles cargados; preservar selección individual y explicar paginación.
- [x] 4. Estado de archivo: STRM creado, pendiente o fallo; diferenciarlo de TMDB y actualizar con los eventos existentes.
- [x] 5. Comprobar un topic: sincronización y comprobación de sus STRM, sin escanear ni reparar otros topics.
- [x] 6. Series progresivas: mostrar archivos cargados; no declarar faltantes hasta terminar la paginación.
- [x] 7. Corregir TMDB de películas ya asociadas mediante el diálogo existente.
- [x] 8. Resultados accesibles: indicar resultado/error junto a las acciones y enlazar Actividad.

## Fuentes oficiales consultadas

- React Router: https://reactrouter.com/start/data/routing — rutas, loaders y redirecciones.
- Telegram: https://core.telegram.org/api/updates — actualización y recuperación de diferencias.
- TMDB: https://developer.themoviedb.org/docs/search-and-query-for-details — buscar candidatos y consultar detalles por identidad.
- W3C: https://www.w3.org/WAI/WCAG22/Understanding/status-messages.html — resultados y progreso accesibles sin mover el foco.

Las decisiones de diseño son propias del proyecto; estas fuentes respaldan los mecanismos, no prescriben una pestaña Biblioteca.

## Auditoría por tarea

La compilación no sustituye la comprobación visual ni la prueba de operaciones reales.

### 1. Biblioteca
Comprobado en navegador: `/library` redirige al grupo principal actual y mantiene sus rutas de grupo/topic/archivo. Sin grupo configurado ofrece Ajustes; un fallo de la API no se interpreta como ausencia de grupo. Acceso móvil desde el menú de cuenta.

### 2. Filtros
Comprobados con datos reales: Por revisar deja el topic Películas con incidencias; Sin STRM vacía la serie Linternas con 5/5 exportados. Se combinan con la búsqueda, no afectan a la vista normal y muestran un estado vacío específico.

### 3. Selección
Comprobado: Visibles selecciona únicamente el topic filtrado, no el grupo completo. Archivos usa resultados visibles cargados, conserva selección individual y aclara el alcance. Sin botones ni enlaces anidados durante la selección.

### 4. Estados STRM
La API deriva estado y error de las exportaciones existentes y de la firma procesada; no usa TMDB como prueba de exportación. Comprobados 5 estados creados en Linternas, archivos escritos y con error en Películas. Los eventos actualizan todos los estados cargados con una consulta local agrupada, sin peticiones adicionales a Telegram.

### 6. Carga progresiva
Comprobado al recargar ALF: 50 archivos visibles mientras sigue la paginación, ningún faltante prematuro. Al terminar: 98 archivos reales; episodios ordenados por temporada/capítulo. Error de paginación detiene el reintento automático y ofrece Reintentar carga; respuestas de otra ruta no contaminan la vista actual.

### 7. TMDB
Abierto el diálogo de Corregir TMDB de una película ya asociada en móvil. Muestra el archivo seleccionado y candidatos; usa el endpoint existente de asociación por ID. No se cambió ninguna coincidencia real para hacer la prueba.

### Hallazgo previo fuera del alcance
En ALF, un archivo llamado `S01E16-E17` cubre dos episodios, pero la detección existente solo reconoce uno y marca E17 como faltante. No lo ha causado esta mejora; requiere revisar el soporte de archivos multiepisodio antes de modificar su exportación.

### 5. Comprobación de topic
Probada en Linternas: 5 STRM revisados, 0 ausentes y 0 incidencias. La consulta restringe todos los conteos de exportación al grupo/topic elegido; no incluye colas globales. Escanea solo ese topic mediante el sincronizador existente, bloquea comprobaciones simultáneas del mismo topic y mantiene la reconciliación automática habitual. Ninguna reparación directa ni borrado nuevo.

### 8. Resultados y Actividad
Comprobada la animación durante el proceso y el resultado al finalizar. El resultado puede cerrarse y enlaza Actividad; la comprobación aparece allí con cantidades y enlace al topic. Los errores se anuncian con alert y los resultados con status. Una respuesta tardía no escribe resultados en la nueva ruta.

## Validación responsive y técnica
- Navegador integrado: 320×740, 390×844, 768×1024 y 1440×900. Revisados filtros, botones, selección, menú móvil, diálogo TMDB y temas claro/oscuro.
- A 320 px todos los botones seleccionados caben en una barra de 48 px; el contenedor respeta el margen derecho. Corregida la interferencia del degradado sobre los filtros.
- Reservado el mismo espacio de información en las tarjetas de películas en gestión; sin modificar proporción o tamaño de los posters.
- Compilación TypeScript/Vite y suite completa de Go pasan; `git diff --check` sin incidencias.
- No se modificaron migraciones, rutas STRM, credenciales, configuraciones de despliegue ni coincidencias TMDB reales. Se hizo una comprobación manual real y se registró en Actividad.
- No se forzaron fallos de Telegram ni se realizaron pruebas destructivas. La recuperación de paginación se auditó en código.
- Cambios locales de desarrollo; todavía no publicados en GitHub ni incluidos en un release.

## Ajuste posterior: estado único de películas
- Solo en gestión de películas del grupo principal: reemplazados título TMDB duplicado, estado y acción separados por una única píldora a todo el ancho interior. Etiquetas Generado, Revisar, Error y Pendiente, con texto/icono además del color (https://www.w3.org/WAI/WCAG22/Understanding/use-of-color.html).
- Se mantienen tamaños del póster y tarjeta, controladores de revisión TMDB y datos del backend. En selección la píldora es informativa para evitar controles interactivos anidados.
- Auditoría visual escritorio y móvil 390 px: alineación inferior uniforme; en móvil las seis primeras píldoras miden 28 px y coinciden con el ancho de su contenedor; cero controles interactivos anidados.
- Compilación TypeScript/Vite y revisión de espacios pasan. No se cambiaron backend, base de datos, STRM ni coincidencias. La apertura del diálogo en esta comprobación no se pudo verificar en vivo por tiempos agotados del navegador integrado; el onClick conserva openMovieMatch(message).

### Compactación del pie de las tarjetas
Eliminada la altura mínima antigua de 125/108 px en gestión de películas. Se reserva siempre la segunda línea del nombre de archivo aunque no se cumpla la condición anterior de pie y nombre presentes; fecha en tercera línea y separación de 8 px antes del estado. Verificado en navegador con Spider-Man: fecha alineada con tarjetas completas, póster sin cambios. Compilación y diff correctos.

### Auditoría de rutas de Biblioteca — 2026-10-02

- [x] `/library`, `/library/manage`, `/library/topics/:topicId`, `/library/topics/:topicId/manage` y `/library/topics/:topicId/videos/:videoId` conservan su contexto sin redirigir a `/groups/:groupId`.
- [x] El loader resuelve el grupo principal configurado antes de renderizar y conserva la comprobación de acceso. La API, los IDs y los eventos WebSocket existentes se reutilizan sin migraciones.
- [x] Enlaces de tarjetas, detalle y accesos laterales respetan el contexto Biblioteca; las rutas de exploración de grupos siguen disponibles.
- [x] La pestaña superior activa depende de la sección visitada, no de que el grupo explorado sea el principal.
- [x] Gestión conserva el modo al navegar y usa reemplazo de historial: comprobado en navegador `/library` → topic 44 → gestión → Atrás vuelve a `/library` con gestión activa.
- [x] Compilación TypeScript/Vite y `git diff --check` correctos.

Referencia oficial: https://reactrouter.com/start/data/route-object (loaders y datos de rutas).
