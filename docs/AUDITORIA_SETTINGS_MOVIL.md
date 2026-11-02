# Configuración móvil — 6 octubre 2026

1. Estado inicial (390 px): copias de seguridad medía 314 px pero su contenido alcanzaba 383 px. La columna `1fr` conservaba el mínimo intrínseco de textos y acciones, recortando botones.
2. Corrección: columna `minmax(0, 1fr)`, texto divisible y acciones que admiten varias líneas únicamente en el breakpoint móvil. Restaurar ocupa una fila propia en móvil. Los mensajes de salud ya no se truncan.
3. Verificación: mediciones DOM de tarjetas y descendientes a 390 y 320 px sin desbordamientos horizontales. Comprobadas las secciones de cuenta, salud, copias, biblioteca principal, destinos, estructura, modo ambiente, TMDB y caché. Tarjetas comprobadas también a 1440 px sin desbordamiento.

Cambios limitados a estilos de Configuración. No se guardaron ajustes, cambiaron credenciales, restauraron copias ni borraron datos. Los selectores nativos conservan su comportamiento habitual; este ajuste no los reemplaza.

Captura del resultado móvil: `/tmp/tgstrm-settings-mobile-audited.png` (local, no destinada a GitHub). La revisión de geometría no equivale a certificar accesibilidad completa ni probar Safari físico.

Se observó además el mensaje «ruta de biblioteca no válida» en la sección de limpieza. No está causado por el desbordamiento y queda fuera de esta corrección visual; no se ejecutó ninguna limpieza.
