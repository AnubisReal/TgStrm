# Filtros de revisión de réplicas

11 de octubre de 2026.

## Causa

La API presenta un topic como `checking` cuando existe una comprobación pendiente del origen, conservando su coincidencia TMDB. El frontend buscaba duplicados únicamente entre topics `eligible`. Por tanto, una comprobación ocultaba conflictos reales sin resolverlos: Forever y Fibrilando desaparecían de «Duplicados».

## Corrección

- Detectar coincidencias duplicadas entre las fuentes seleccionadas del mismo cajón, también cuando estén comprobándose. Se excluyen los topics desmarcados, vacíos, incompatibles, ya copiados o en cola.
- «Por revisar» sustituye «Sin asociar»: muestra fuentes incluidas sin coincidencia o con comprobaciones, análisis o documentos pendientes, aunque tengan coincidencia TMDB.
- Conservar la posibilidad de revisar una coincidencia existente durante la comprobación del origen. La API ya admite esta edición sin modificar su inventario.
- Explicar en el aviso qué filtro permite identificar las comprobaciones pendientes. La confirmación mantiene todos sus controles; no se omiten verificaciones de seguridad ni se altera la selección automáticamente.

## Auditoría

`npm run build --prefix web` completó TypeScript y la compilación. La advertencia existente sobre tamaño del bundle no impide compilar.

Comprobación en el navegador con el plan real: «Duplicados» muestra cuatro tarjetas (dos Forever y dos Fibrilando); «Por revisar» muestra las 45 fuentes incluidas pendientes de comprobar; «Revisar coincidencia» abre el diálogo para Fibrilando. La confirmación continúa deshabilitada con esos bloqueos. No se cambió ninguna selección, coincidencia ni se confirmó la réplica durante la auditoría.

El cambio se limita al frontend de revisión: no modifica Telegram, STRM, base de datos, generación, seguimiento de nuevos topics o espejos.
