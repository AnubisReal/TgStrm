# Reconocimiento de películas monitorizadas

## Diagnóstico (9 de octubre de 2026)

Se analizaron los nombres y pies de los 1849 archivos en revisión. 1847 tenían
el motivo genérico de coincidencia no única; dos tenían una ficha incompleta.
Los formatos se solapan: 1387 contienen un año, 250 tienen texto entre
paréntesis, 50 empiezan por un año, siete incluyen IDs TMDB/IMDb, 45 presentan
marcadores de episodio o de archivo partido y seis carecen de nombre y pie.
Los datos privados del análisis se guardan únicamente en `data/audits`, ignorado
por Git; las credenciales no se imprimen ni se copian a informes.

La búsqueda de TMDB incluye títulos alternativos, pero el validador anterior
solo contrastaba el título traducido y el original. Un resultado válido se
rechazaba si el archivo utilizaba un alias de distribución. También aparecieron
años del estreno local que difieren del año principal, títulos bilingües,
metadatos de director, años al comienzo y números de saga.

## Cambios y límites

El reconocedor exacto anterior permanece como primera opción. El complemento
admite años al principio, títulos alternativos entre paréntesis, números romanos
II/III/IV, separadores y diacríticos, palabras pegadas, marcadores Parte/Part
antes de un número y un año pegado a la resolución. El título puede incluir
un año diferente del año final de estreno. Los nombres de director solo se
ignoran si los créditos oficiales confirman el sufijo completo.

Los IDs explícitos TMDB/IMDb se resuelven mediante sus endpoints oficiales;
los IDs contradictorios permanecen en revisión. Dos títulos concatenados deben
corresponder a alias completos de una misma película. Los prefijos sirven
únicamente para buscar candidatos, nunca para aprobar una coincidencia.

Las fechas distintas solo se aceptan si el año figura en los estrenos oficiales
de España. No se acepta el simple año más próximo. No se fusionan archivos
partidos ni se convierten episodios de series en películas. Las consultas tienen
límites y memoria temporal por archivo; una verificación incompleta o ambigua
permanece manual. Se mantiene el limitador global de TMDB y el tratamiento de
fallos temporales.

La migración 059 reanaliza exclusivamente revisiones automáticas no asociadas,
no confirmadas y no enviadas, en monitores activos. No cambia entregas
completadas, selecciones manuales, vídeos, pies ni STRM existentes. El envío
posterior utiliza las etapas persistentes y la protección de documentos
idénticos ya implementadas; no se crea otra vía de reenvío.

## Validación

Pruebas unitarias de extracción, IDs contradictorios, secuelas, títulos pegados,
alias completos y créditos. Pruebas con respuestas controladas del proveedor
para ambigüedades, presupuesto agotado, estrenos ES y fechas de otros países.
Prueba de migración en PostgreSQL dentro de una transacción revertida para
preservar todas las entregas protegidas. Pruebas completas de Go y go vet.
La comparación con TMDB es de lectura: no envía películas ni guarda matches.

## Fuentes oficiales

- https://developer.themoviedb.org/reference/search-movie
- https://developer.themoviedb.org/reference/movie-alternative-titles
- https://developer.themoviedb.org/reference/find-by-id
- https://developer.themoviedb.org/reference/movie-release-dates
- https://developer.themoviedb.org/reference/movie-credits

Resultado medido con la misma muestra de 55 pendientes: el validador anterior
asociaba uno y el complemento auditado asocia 20. Es una muestra, no una
estimación del número final recuperable. El resto del catálogo se reanaliza
mediante el trabajador persistente. Tras activar la migración se verificaron
3473 entregas completadas intactas, 66 excluidas intactas y cero documentos
activos duplicados en el destino.
