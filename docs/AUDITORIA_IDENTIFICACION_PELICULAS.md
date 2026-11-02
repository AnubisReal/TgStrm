# Auditoría de identificación de películas · 7 de octubre de 2026

## Evidencia y causas

Se recorrieron en modo lectura los 4.443 registros pendientes. 2.566 nombres contenían guiones bajos; 20 no tenían nombre de archivo; 22 pies superaban 300 caracteres. La primera línea no equivale siempre al título: Willow incluía una sinopsis separada por muchos espacios y producía HTTP 400. La limpieza anterior tampoco reconocía adecuadamente años entre corchetes, pegados a etiquetas técnicas o nombres separados por puntos/guiones bajos. Los conteos describen formatos, no garantizan una coincidencia TMDB.

La nueva limpieza aporta evidencia distinta para 4.066 archivos. Se detectaron 32 casos con marca de episodio o nombre de parte dividida que permanecen para revisión; no se asume que sean películas completas. La muestra remota inicial de 45 registros recuperó 15 coincidencias únicas, sin guardar asociaciones durante la prueba.

## Cambios y límites

- Limpieza adicional limitada al monitor de películas: separadores, extensión de vídeo, año, sufijos técnicos y separación marcada entre título y sinopsis. Mantiene el título numérico y no inventa un año.
- Consulta tanto el pie como el nombre. Conserva evidencia previa y rechaza identidades contradictorias. Comparación de título/original y año tolera acentos vocálicos; IDs distintos coincidentes siguen siendo ambiguos. No se elige el primer resultado por popularidad.
- Búsquedas excesivamente largas no se envían. HTTP 400/422 pasa a otra evidencia o revisión, sin reintento infinito. Otros estados HTTP se muestran de manera diferenciada; errores de conexión se sanitizan sin divulgar la clave.
- Episodios y partes divididas permanecen para revisión. No se reconstruyen partes, cambian archivos ni generan asociaciones inventadas. Los títulos alternativos, errores ortográficos o años dudosos pueden seguir necesitando decisión humana.
- No se modifica la identificación de series ni la limpieza global de TMDB. Tampoco se cambia la presentación de Telegram ni se eliminan mensajes, STRM o carpetas.

## Aplicación y verificación

Copia privada de PostgreSQL previa. Reencolados 4.411 registros en revisión, sin ID TMDB confirmado, sin lease y con firma actual coincidente, exclusivamente del origen seleccionado. Las 944 entregas completadas al inicio y las asociaciones existentes no se reiniciaron. El reanálisis completo corre en segundo plano; ya se observó generación de nuevos STRM. No se afirma que todos los registros hayan encontrado película.

Pruebas de formatos reales, títulos numéricos, acentos, conflictos, multipartes y episodios: correctas. `go test -race ./...`, `go vet ./...` y pruebas PostgreSQL de consultas/barreras: correctos. No hubo modificaciones en la interfaz en este ajuste.

## Fuentes

- [TMDB: búsqueda textual e identificadores externos](https://developer.themoviedb.org/docs/finding-data)
- [TMDB: errores HTTP y parámetros inválidos](https://developer.themoviedb.org/docs/errors)

La extracción de nombres es una decisión de ingeniería de TgStrm, no un formato de nombres prescrito por TMDB.
