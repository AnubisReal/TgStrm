# Búsqueda manual por ID de TMDB

13 de octubre de 2026.

El endpoint compartido de búsquedas manuales solo consultaba `search/tv` o `search/movie`: un ID escrito en el campo se interpretaba como título.

Ahora un entero positivo de 32 bits consulta los detalles del tipo seleccionado, reutilizando los métodos existentes de localización y las credenciales cifradas. También se admite `tmdb:1396` como referencia explícita. Un número inexistente vuelve a la búsqueda por título; una referencia explícita inexistente devuelve resultados vacíos. Un título puramente numérico cuyo número coincide con un ID existente se interpreta como ID; para buscar ese título puede añadirse su año, como en la búsqueda existente. No se aceptan rutas arbitrarias ni se cambia la selección automáticamente.

Los campos de revisión del plan y asociación de series/películas indican «Título o ID de TMDB». La asociación final sigue requiriendo elegir el resultado y conserva sus validaciones y sincronización.

Auditoría: pruebas de identificación de entradas, consulta real del ID de serie 1396 sin escrituras ni asociación, pruebas generales de Go, análisis estático, compilación de API y frontend, y comprobación de espacios del diff. La prueba real devolvió un solo resultado con el ID solicitado y título no vacío.

Fuentes: [detalles de series](https://developer.themoviedb.org/reference/tv-series-details), [detalles de películas](https://developer.themoviedb.org/reference/movie-details). La consulta por ID utiliza los endpoints oficiales de detalles, en lugar de buscar el número como título.
