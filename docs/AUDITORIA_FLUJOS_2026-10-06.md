# Auditoría de flujos — 6 de octubre de 2026

## Alcance y límites

Revisión interna de identificación de episodios, asignación manual, normalización de pies, rematcheo, generación y retirada de STRM, replicación, espejos, colas persistentes, eventos WebSocket, paginación y acceso. No equivale a probar todas las combinaciones reales de Telegram ni todos los dispositivos.

No se han ejecutado nuevas replicaciones masivas ni borrados en Telegram como parte de esta auditoría. Los tests de archivos utilizan directorios temporales; las pruebas de PostgreSQL específicas se revierten con rollback.

## Correcciones verificadas

- **TMDB temporalmente inaccesible:** se distingue de un episodio realmente no identificado. La normalización conserva el pie y el STRM, y devuelve un error recuperable en vez de borrar información útil.
- **Carpeta compartida de recuperación:** `.tgstrm-recovery` nunca se selecciona como árbol completo de un topic eliminado. Se mantiene el borrado habitual de carpetas propias de los topics.
- **Cierre entre mover un STRM y confirmar en PostgreSQL:** el reintento reconoce el respaldo por su contenido, completa la operación sin sobrescribirlo y rechaza rutas ajenas o respaldos alterados.
- **Asignaciones manuales simultáneas:** la comprobación de conflictos se realiza dentro de la transacción y del bloqueo del topic. Solo cuentan las asignaciones de la serie TMDB actual.
- **Acceso durante un reinicio de API:** la pantalla de Telegram ofrece reintentar cuando falla la comprobación, en lugar de seguir mostrando indefinidamente «Comprobando tu sesión» junto al error.

## Flujos revisados

| Flujo | Evidencia y observaciones |
| --- | --- |
| Identificación | Se conservan los formatos existentes y la prioridad de pies y asignaciones manuales. Los errores de contexto no son evidencia de episodio. |
| Normalización | La edición remota se confirma antes de actualizar el catálogo; se protege frente a cambios posteriores del usuario. |
| Rematcheo | Trabajo persistente por fases y bloqueo por topic; se evita tratar una corrección como sustitución destructiva de un vídeo. |
| STRM | Se verifica propiedad y contenido; se protege recuperación y reanudación tras interrupción. |
| Replicación | Manifest persistido, lease y heartbeat; el trabajo no depende de que la vista permanezca abierta. |
| Orden de envío | Orden por temporada y episodio reconocido; orden cronológico como fallback cuando no hay identificación segura. |
| Espejos | Bloqueo compartido con correcciones de identidad y comparación del pie de origen/aplicado; las bajas del origen no borran las copias conservadas. |
| Tiempo real | Suscripción WebSocket con timeout de escritura; actualización del frontend y paginación revisadas por código. Falta comprobación visual autenticada en esta sesión. |
| Catálogo | El cursor de paginación permanece separado de la actualización de la primera página. |
| Colas | Los trabajos históricos con destino eliminado o inaccesible siguen mostrando su causa; no se recrean destinos eliminados automáticamente. |
| Acceso | API y proxy responden; tras recargar aparece el formulario de administración. No se ha utilizado la contraseña del usuario. |

## Verificación

- `TGSTRM_TEST_DATA_DIR="$PWD/data" GOCACHE=/tmp/tgstrm-go-cache go test -race ./...`: correcto tras las correcciones del backend.
- `GOCACHE=/tmp/tgstrm-go-cache go vet ./...`: correcto.
- Compilación TypeScript/Vite correcta después del último ajuste del botón de reintento.
- Tests adicionales de indisponibilidad del contexto, árbol de recuperación compartido, reanudación del respaldo y conflicto de asignación por serie: correctos.

Algunas pruebas de integración externas requieren activación explícita y no se ejecutan con la suite general. Pendiente: recorrido visual autenticado en escritorio/móvil y comprobación de eventos reales sin recargar.

## Referencias oficiales

- [Go: os.Root y operaciones confinadas](https://pkg.go.dev/os#Root).
- [Telegram: errores RPC y FLOOD_WAIT](https://core.telegram.org/api/errors).

El subdominio de desarrollo se autoriza únicamente mediante una variable de entorno del proceso Vite; no se añade a la configuración versionada.
