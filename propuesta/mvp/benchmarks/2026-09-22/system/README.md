# CPU en reposo e I/O durante Evaluate

**Resultado local:** CPU en reposo mediana **0,002676 % de una CPU** con un programa de 10.000 condiciones distintas cargado. En **4.100 evaluaciones**, el trazador no observa accesos a archivos, lecturas/escrituras de descriptores ni llamadas de red dentro de las ventanas de evaluación. Los controles positivos sí detectan la apertura de un archivo y la creación de un socket.

Esto aporta la evidencia de sistema pendiente de F1 para los escenarios medidos. No cierra F1: faltan el corpus aprobado, la evidencia remota y los demás criterios de aceptación. No se modificó código de producción.

## Entorno y reproducción

Mismo host Apple M4 y restricciones de los benchmarks anteriores: Linux arm64, 1 CPU, 512 MiB, sin swap, red deshabilitada, filesystem de solo lectura, `GOMAXPROCS=1`, `GOMEMLIMIT=448MiB`, Go 1.27.1 y CEL v0.30.0. La imagen de diagnóstico usa Debian bookworm y strace 6.1, en lugar de la imagen scratch de throughput; estas muestras no sustituyen ni comparan latencias con aquella imagen.

```sh
bash scripts/measure-system.sh bin/system
```

Ejecutar desde la raíz. Requiere Go, Docker, Python 3 y `shasum`. La preparación de imagen descarga strace; las mediciones se ejecutan sin red. El trazador necesita `SYS_PTRACE` y escribe sus registros en el directorio de evidencia montado. Se conserva el ID exacto de imagen, kernel, herramientas, commit base, estado del árbol y hashes de fuentes/binario en [environment.txt](environment.txt). El árbol aún contiene cambios sin commit; la etiqueta Docker local es mutable y no identifica por sí sola esta captura.

El [diagnóstico Linux](../../../../../engine/system_test.go) es optativo (`RULEFARE_SYSTEM=1`) y reutiliza los fixtures de benchmarks. La suite habitual lo omite. El [runner](../../../../../scripts/measure-system.sh) falla si el test falla, faltan ventanas/controles o aparece una llamada de I/O inesperada. No aplica un umbral automático de CPU para una ventana tan breve.

## CPU en reposo

Cada repetición inicia un proceso nuevo, compila 10.000 condiciones distintas, ejecuta GC/liberación de memoria, espera un segundo de asentamiento y mantiene vivo el programa durante una espera de diez segundos. `getrusage(RUSAGE_SELF)` mide CPU de usuario y sistema de todo el proceso, incluidos los threads del runtime. La captura y el log quedan fuera del intervalo; no hay strace en estas ejecuciones.

Porcentaje = `100 × (delta CPU usuario + delta CPU sistema) / tiempo transcurrido`. Los valores de CPU de Linux tienen resolución de microsegundos. No se usa el porcentaje instantáneo de `docker stats`.

| Proceso | Tiempo observado | CPU usuario | CPU sistema | % de una CPU |
|---|---:|---:|---:|---:|
| 1 | 10,013104 s | 331 µs | 0 µs | 0,003306 % |
| 2 | 10,014963 s | 255 µs | 5 µs | 0,002596 % |
| 3 | 10,014919 s | 264 µs | 4 µs | 0,002676 % |

Mediana **0,002676 %**, rango **0,002596–0,003306 %**: compatible con el objetivo de CPU ≈ 0 para el programa cargado en reposo. Incluye el coste de despertar del temporizador y actividad residual del runtime. No demuestra cero absoluto ni caracteriza pausas de GC forzado en ventanas de varios minutos, un servidor completo o cargas ajenas al motor. [Datos originales](idle.txt).

## I/O observado

`strace -f` sigue todos los threads desde el inicio y registra las categorías `%file`, `%network` y `%desc`, incluidos intentos fallidos. Marcadores escritos a stderr delimitan cada ventana: después de compilar y antes del primer `Evaluate`, hasta terminar la última llamada. Se excluyen únicamente las escrituras de los marcadores; los logs del test, la carga de fixtures y la compilación ocurren fuera de las ventanas. No hay calentamiento que pueda ocultar I/O de la primera evaluación.

| Escenario | Evaluaciones | Resultado esperado | Accesos a archivos/red |
|---|---:|---|---:|
| 100 condiciones repetidas | 1.000 | `MATCH` | 0 |
| 100 condiciones distintas | 1.000 | `MATCH` | 0 |
| 10.000 condiciones distintas | 100 | `MATCH` | 0 |
| Entrada inválida | 1.000 | `INVALID_INPUT` y error | 0 |
| Condición falsa | 1.000 | `NO_MATCH` | 0 |

Se observan **37 reservas `mmap` anónimas**, siempre con `MAP_ANONYMOUS` y descriptor −1, y **191 esperas `epoll_pwait`** del runtime. No son lecturas de archivos ni intercambios de red. El analizador verifica los argumentos de `mmap`; rechaza un mapeo respaldado por archivo. No se confunde «cero I/O» con «cero llamadas al kernel».

El control abre `testdata/schema.json` y crea/cierra un socket `AF_INET` fuera de las ventanas del motor. Confirma que la instrumentación detectaría intentos aunque la red esté deshabilitada. Además, se verificó que el analizador rechaza trazas con apertura de archivo, creación de socket, escritura y mmap de archivo inyectados, y acepta una ventana sin I/O: [comprobaciones del analizador](parser-checks.txt).

Evidencia: [resumen](io-summary.txt), [traza completa](syscalls.txt), [ejecución y número de llamadas](evaluation.txt). Las categorías observan accesos explícitos mediante syscalls; no miden tráfico físico de paginación o del host Docker. Esta muestra no cubre todos los inputs posibles, ni prueba por sí sola todas las rutas de empate o agotamiento; esas rutas conservan sus pruebas funcionales existentes.

## Verificación

Runner completo en verde, suite del motor ejecutada en Linux sin activar diagnósticos en verde, `GOOS=linux GOARCH=arm64 go vet ./...`, sintaxis Bash y `git diff --check` sin errores. Se conservaron los límites runtime y los fixtures originales. La ejecución remota de estas mediciones sigue pendiente; este runner todavía se invoca manualmente.
