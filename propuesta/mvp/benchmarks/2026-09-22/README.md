# Baseline de rendimiento · 2026-09-22

Registro histórico T3.5. La [medición posterior T3.6](./t3.6/README.md) conserva esta baseline y documenta la optimización y sus límites.

T3.5 implementada y medida. La latencia objetivo pasa en el escenario medido; **el RSS observado tras GC supera 128 MiB**, por lo que el presupuesto de memoria de F1 sigue abierto y requiere T3.6. No se cambió el motor para optimizar estos resultados.

## Entorno y trazabilidad

- Host: Apple M4, macOS arm64. Go 1.27.1; CEL Go v0.30.0.
- Medición controlada: Linux arm64 en Docker Desktop 29.7.2, cuota de 1 CPU, límite duro de 512 MiB, sin swap ni red; `GOMAXPROCS=1`, `GOMEMLIMIT=448MiB` para dejar margen al resto del proceso.
- Medición local adicional: Darwin arm64, `GOMAXPROCS=1`, `GOMEMLIMIT=512MiB`. Estos parámetros por sí solos no imponen una cuota de CPU ni un límite duro de memoria.
- Base Git: `6c86f4df4dbcbc023834ddb9ac97440de17e6dda` con cambios sin commit. Cada ejecución conserva estado del árbol y hashes SHA256 del motor, pruebas, fixtures, módulo y runner. Los números corresponden a esas fuentes, no a HEAD sin modificaciones.
- Tres repeticiones para throughput, compilación y percentiles; una ejecución en proceso nuevo para memoria. Las tablas usan la mediana de las tres repeticiones cuando corresponde.

## Escenario

Todas las reglas de la capa son candidatas y hacen match. Cada regla tiene scope `country` + `supplier`, prioridad única, un outcome percentage y la condición `active && amount_minor >= 100000 && travel_date >= timestamp('2026-01-01T00:00:00Z')`. El contexto contiene las 12 variables del fixture, con enteros/timestamps Go ya tipados.

`BenchmarkEvaluate` excluye compilación y construcción de inputs del tiempo y las asignaciones. Incluye la validación/normalización del contexto, CEL, ranking, copia del outcome y trace completo; no incluye serialización JSON. Comprueba el ganador y la cantidad de candidatos. El caso de 10.000 candidatas es de estrés: el presupuesto de latencia se aplica a 100.

`BenchmarkEvaluateLatency100` se ejecuta por separado: 100 llamadas de calentamiento y 10.000 observaciones por repetición, percentiles por rango más cercano. Incluye pausas de GC y planificación; se informa el tiempo de cada llamada, sin serialización. Las lecturas del reloj son parte de esta medición, no del benchmark de throughput.

## Linux arm64 · 1 CPU / 512 MiB

| Operación | Tiempo mediano | Bytes/op | Asignaciones/op |
|---|---:|---:|---:|
| Evaluar 10 candidatas | 10,600 µs | 6.664 | 74 |
| Evaluar 100 candidatas | 140,791 µs | 47.598 | 437 |
| Evaluar 10.000 candidatas | 21,212 ms | 5.804.811 | 40.048 |
| Compilar 10.000 reglas | 575,316 ms | 363.478.756 | 5.500.036 |

| Métrica | Observado | Objetivo / interpretación |
|---|---:|---|
| p95, 100 candidatas | 0,497 ms (máximo entre repeticiones: 0,498 ms) | ≤ 5 ms: pasa en esta medición |
| p99, 100 candidatas | 0,643 ms (máximo entre repeticiones: 0,834 ms) | ≤ 10 ms: pasa en esta medición |
| Incremento de heap retenido tras GC | 53,5 MiB | No equivale al RSS; incluye inicialización retenida de librerías |
| RSS absoluto tras GC | **136,7 MiB** | Supera el presupuesto de 128 MiB |
| RSS tras `debug.FreeOSMemory` | 104,7 MiB | Diagnóstico; no acredita el comportamiento normal de la aplicación |

El RSS incluye el runtime y el ejecutable de pruebas. Se lee de `/proc/self/statm` con el programa aún vivo. La medición de memoria usa un proceso nuevo, recoge basura para descartar los objetos temporales de compilación y después mide por separado el efecto de forzar la devolución de páginas al sistema. No es una medida del pico durante compilación. `ns/op` de este benchmark mezcla compilación, GC y observación: no debe usarse como latencia del motor.

Datos originales: [entorno y hashes](./linux-arm64-container/environment.txt), [throughput y asignaciones](./linux-arm64-container/throughput.txt), [percentiles](./linux-arm64-container/latency.txt), [memoria](./linux-arm64-container/resident.txt).

## Darwin arm64 · medición local adicional

| Operación | Tiempo mediano | Bytes/op | Asignaciones/op |
|---|---:|---:|---:|
| Evaluar 10 candidatas | 9,454 µs | 6.664 | 74 |
| Evaluar 100 candidatas | 130,314 µs | 47.598 | 437 |
| Evaluar 10.000 candidatas | 15,153 ms | 5.804.810 | 40.048 |
| Compilar 10.000 reglas | 458,208 ms | 363.470.421 | 5.500.030 |

p95/p99 para 100 candidatas: 0,358 / 0,384 ms. Heap retenido: 53,5 MiB. RSS leído mediante `ps`: 147,0 MiB tras GC y 147,1 MiB tras `FreeOSMemory`. La devolución de memoria al sistema no garantiza que el RSS observado baje inmediatamente en todas las plataformas.

Datos originales: [entorno y hashes](./macos-arm64/environment.txt), [throughput y asignaciones](./macos-arm64/throughput.txt), [percentiles](./macos-arm64/latency.txt), [memoria](./macos-arm64/resident.txt). No comparar diferencias entre Darwin y Linux como regresiones.

## Reproducir y seguir

Desde la raíz del repositorio:

```sh
bash scripts/benchmark.sh bin/benchmarks-local
bash scripts/benchmark.sh bin/benchmarks-container --container
```

El modo contenedor necesita Docker y un toolchain Go local con las dependencias resueltas. Compila el binario de pruebas para Linux y crea una imagen `FROM scratch`; no descarga imágenes base. Cada grupo de métricas usa un proceso/contenedor nuevo. El runner produce `environment.txt`, `throughput.txt`, `latency.txt` y `resident.txt`.

CI ejecuta el modo contenedor y guarda estos cuatro archivos como artefacto asociado al SHA. La ejecución remota aún no se ha verificado. Una baseline Linux amd64 del CI será distinta de esta Linux arm64. La detección automática de regresiones del 10 % requiere fijar un entorno comparable; por ahora se conservan las mediciones para comparación en el mismo entorno.

El siguiente paso es **T3.6: perfilar el exceso de RSS observado**, distinguiendo heap vivo, temporales de compilación y páginas retenidas por el runtime antes de decidir cambios. También queda cuantificado el coste de recorrer 10.000 candidatas: ~5,8 MB asignados por evaluación. CPU en reposo e I/O no se han medido con instrumentación de sistema en esta entrega. F1 permanece abierta.
