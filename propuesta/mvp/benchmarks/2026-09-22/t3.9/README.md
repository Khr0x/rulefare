# T3.9 · Límites runtime, payload y pruebas adversariales

Registro de la primera entrega. La [optimización posterior](../t3.9-optimization/README.md) corrige la regresión de la baseline manteniendo las protecciones; estas mediciones se conservan como referencia histórica.

Se implementaron los [límites v1 del motor](../../../../../engine/LIMITS.md), se probaron entradas adversariales y se midieron tanto el rechazo como el sobrecoste en el entorno de F1. La latencia absoluta y el RSS siguen dentro del objetivo en los escenarios habituales medidos. **El tiempo y las asignaciones aumentan respecto a T3.6: no se cumple el umbral de regresión del 10 %.** T3.9 tiene evidencia local; el cierre de rendimiento de F1 conserva esa revisión pendiente.

## Entorno y método

Apple M4; Linux arm64 en Docker Desktop 29.7.2; Go 1.27.1; CEL v0.30.0; imagen `scratch`; cuota de 1 CPU y 512 MiB duros, sin swap/red; `GOMAXPROCS=1`, `GOMEMLIMIT=448MiB`. [Entorno, imagen y hashes](environment.txt). Árbol con cambios sin commit; los hashes identifican las fuentes medidas. No hubo fuzzing simultáneo con la corrida final.

Tres repeticiones de cada benchmark y de percentiles; mediana salvo indicación contraria. RSS de condiciones únicas: tres procesos nuevos. RSS de repetidas: un proceso nuevo. Los benchmarks excluyen la construcción de las entradas y la compilación cuando miden `Evaluate`; incluyen validación, coste CEL, ranking y trace. No miden serialización JSON. No se ha verificado el workflow remoto.

## Cotas y comportamiento

- Contexto: 256 entradas como máximo, strings de 4 KiB y 64 KiB agregados de texto. Compilación: 8 MiB agregados de texto y cardinalidades preexistentes de F1. Se rechazan excesos antes de ordenar claves, convertir números o entrar a CEL.
- Outcomes: 38 dígitos totales y 18 decimales como máximo. `json.Number` debe ser un entero `int64` de hasta 20 bytes. Timestamp textual de hasta 35 bytes.
- CEL: 10.000 unidades por condición y 1.000.000 acumuladas por evaluación. Si se agotan, el resultado es `LIMIT_EXCEEDED` con error tipado, sin ganador ni trace parcial, incluso con un fallback ya evaluado.
- Regex: patrón de hasta 256 bytes antes de ejecutar RE2; aplica a métodos, llamadas globales, `dyn`, constantes y patrones calculados. La cancelación no puede quedar oculta por `|| true`.

Las unidades CEL estiman trabajo, no son un timeout ni un límite de memoria. CEL cobra después de cada operación; el acumulado del motor se revisa entre condiciones. Las cotas de texto no incluyen objetos Go ya creados por el llamador. Los detalles y formatos de errores están en el contrato de límites.

## Hallazgo que cambió la implementación

Solo activar `cel.CostLimit` no bastaba para una regex de 4 KiB sobre un string de 4 KiB: el matcher tardó **40,551 ms/op** antes de cancelarse, con **814.296 B/op**. El chequeo previo de patrón reduce ese mismo rechazo a **2,796 µs/op**, con **1.768 B/op**. Se conservan la [medición previa](limits-before-regex-guard.txt), su [entorno y hashes](environment-before-regex-guard.txt) y la [final](limits.txt). No se introdujo un matcher alternativo: los patrones admitidos pasan al matcher original de CEL.

## Resultados adversariales

| Escenario | Mediana | Bytes/op | Asignaciones/op | Resultado |
|---|---:|---:|---:|---|
| String en límite de 4 KiB, condición `true` | 2,621 µs | 1.840 | 32 | MATCH |
| String de contexto de 1 MiB | 0,219 µs | 176 | 4 | INVALID_INPUT antes de CEL |
| `contains` que agota coste individual | 5,454 µs | 1.848 | 36 | LIMIT_EXCEEDED |
| Patrón regex de 4 KiB | 2,796 µs | 1.768 | 32 | LIMIT_EXCEEDED antes de RE2 |
| Regex y string de 256 bytes | 21,925 µs | 29.727 | 71 | MATCH |
| Coste acumulado, 3.000 reglas disponibles | 3,743 ms | 1.242.425 | 26.733 | LIMIT_EXCEEDED antes de acabar las reglas |
| Decimal de outcome de 1 MiB | 0,249 µs | 184 | 4 | Compile devuelve LIMIT_EXCEEDED |

Estos son tiempos medios por operación de cada repetición, no percentiles ni cotas máximas. La repetición más lenta del caso acumulado fue de 6,067 ms/op. Los casos no representan todo el espacio de expresiones permitidas.

## Impacto en la carga habitual

Comparación con [T3.6, condiciones únicas](../t3.6-unique/README.md), mismo host y límites:

| Métrica | T3.6 | T3.9 |
|---|---:|---:|
| Evaluar 10 candidatas | 10,212 µs | 17,031 µs |
| Evaluar 100 candidatas | 127,339 µs | 205,767 µs |
| B/op y asignaciones/op, 100 | 47.598 / 437 | 93.210 / 1.637 |
| Evaluar 10.000 candidatas | 8,369 ms | 22,057 ms |
| B/op y asignaciones/op, 10.000 | 5.804.974 / 40.050 | 10.365.242 / 160.054 |
| Compilar 10.000 repetidas | 14,521 ms | 19,157 ms |
| Compilar 10.000 únicas | 429,322 ms | 530,985 ms |
| p95 / p99, 100 candidatas | 0,417 / 0,434 ms | **0,482 / 0,606 ms** |
| RSS tras GC, 10.000 repetidas | 31,4 MiB | **31,4 MiB** |
| RSS tras GC, 10.000 únicas, mediana | 86,4 MiB | **94,6 MiB** |
| Heap retenido, 10.000 únicas, mediana | 23,0 MiB | 27,7 MiB |

RSS único: rango **89,6–101,0 MiB**, tres procesos, todos por debajo de 128 MiB. p95/p99 siguen bajo 5/10 ms. El throughput de 10 y 100 candidatas empeora aproximadamente **67 % y 62 %** y aumentan las asignaciones por el seguimiento de coste y las comprobaciones. La carga de 10.000 también empeora; es de estrés y no lleva el presupuesto de latencia de 100 candidatas. Se conserva la evidencia sin declarar resuelto el criterio de regresión del 10 % ni desactivar la protección para mejorar el benchmark. Corresponde revisar ese sobrecoste como pendiente de F1.

Datos: [throughput](throughput.txt), [percentiles](latency.txt), [RSS repetido](resident.txt), [RSS único](unique-resident.txt), [límites](limits.txt). `rss-released-B` es diagnóstico de devolución forzada, no el criterio de aceptación.

## Pruebas y reproducción

Pasan `go test ./...`, `go test -race ./...`, `go vet ./...`, `gofmt` y `git diff --check`. Los tres targets de fuzzing pasaron campañas adicionales de 10 s con dos workers, sin fallos.

Las pruebas cubren límites inclusivos de strings, agregado de contexto, coste individual y precisión decimal; excesos de cardinalidad, agregado de compilación y valores/claves de 1 MiB; rechazo sin fallback; cancelación que no puede quedar oculta; repetición tras fallos; concurrencia con errores de límite; mutación de errores devueltos; y código de salida 1 de la CLI sin datos sensibles. Se añadieron semillas de entradas sobredimensionadas al fuzzing.

```sh
go test ./...
go test -race ./...
go vet ./...
bash scripts/benchmark.sh bin/t39-final --container
```

El runner guarda ahora `limits.txt`, además de throughput, percentiles, RSS y entorno. El upload `*.txt` de CI ya lo incluye. Quedan el corpus aprobado, documentación integral, CPU en reposo/I/O, revisión del sobrecoste y evidencia remota para cerrar F1.
