# Optimización del sobrecoste de T3.9

La evaluación de 100 candidatas de la baseline baja de **205,767 a 19,810 µs**, con todos los límites activos. Las asignaciones bajan de **93.210 a 15.882 B/op** y de **1.637 a 143 asignaciones/op**. La baseline usa condiciones idénticas; el nuevo control con 100 condiciones distintas registra **124,077 µs**. No se extrapola el beneficio de reutilización a reglas distintas.

## Qué indicaron los perfiles

En el [perfil original de asignaciones](before/allocations-top.txt), `Evaluate` atribuye directamente el 42,24 % de los bytes: reserva de supervivientes que copiaban `Rule` completo, crecimiento del trace y un objeto para `errors.As` por candidata incluso sin error. El [perfil CPU](before/cpu-top.txt) registra movimiento de objetos y barreras de GC, además de los observadores de coste CEL. Los porcentajes acumulados se solapan y no deben sumarse.

El primer cambio redujo las 100 candidatas a **107,155 µs**, manteniendo una ejecución CEL por candidata. Su [perfil posterior](intermediate/cpu-top.txt) concentra el 67,68 % acumulado en `evalWatch.Exec` y el 37,29 % en el observador de coste. El caso de 10 candidatas aún tardaba 13,282 µs, frente a 10,212 µs antes de T3.9. Esa medición justificó evitar la repetición de condiciones idénticas dentro de la misma llamada.

## Cambio aplicado

1. El ranking ordena referencias a reglas inmutables, sin copiar el struct completo. Se usa `slices.SortFunc`; especificidad, prioridad e ID forman un orden total porque los IDs son únicos. El ID sigue ordenando la presentación, sin decidir empates de negocio.
2. El trace reserva capacidad para la cantidad conocida de candidatas y evita crecimientos sucesivos. Cada resultado conserva sus propias estructuras mutables; el outcome sigue copiándose.
3. La inspección de cancelación se realiza solo si hay error, evitando una asignación en cada evaluación correcta.
4. `Compile` identifica textos de condición repetidos dentro de cada capa. `Evaluate` reutiliza resultados **sin error** exclusivamente dentro de esa llamada, con los bindings deterministas y escalares actuales. No se comparten resultados entre solicitudes ni se almacenan errores. Las condiciones únicas continúan ejecutando CEL por candidata.

**Cada regla sigue consumiendo el coste completo de su condición**, aunque reutilice el resultado. El presupuesto acumulado se agota en la misma regla; el límite individual, el guard previo de regex, los límites de entrada y la prohibición de fallback por agotamiento permanecen intactos. La prueba diferencial compara contra la evaluación independiente con la reutilización desactivada, incluyendo true/false, cambios de contexto, errores de conversión, regex y agotamiento acumulado.

Perfiles finales de [CPU](after/cpu-top.txt) y [asignaciones](after/allocations-top.txt), aislando `BenchmarkEvaluate/100`. Se conserva el [log del perfil](after/profile-run.txt) para verificar el escenario.

Deltas de [evaluación](evaluate.diff) y [ranking](ranking.diff). Los hashes de todas las fuentes, incluyendo compilación y el indicador privado de reutilización, están en el entorno posterior.

## Resultados finales

Mismo host Apple M4, Linux arm64, Go 1.27.1, CEL v0.30.0, Docker Desktop 29.7.2, 1 CPU, 512 MiB sin swap/red, `GOMAXPROCS=1`, `GOMEMLIMIT=448MiB`. Medianas de tres repeticiones; RSS repetido usa una sola lectura y RSS único tres procesos nuevos. Los perfiles se capturan aparte de los benchmarks y no se ejecuta fuzzing durante las mediciones finales.

| Evaluación, condiciones repetidas | T3.9 inicial | Optimizada |
|---|---:|---:|
| 10 candidatas | 17,031 µs; 11.226 B; 194 asignaciones | **5,986 µs; 3.512 B; 53 asignaciones** |
| 100 candidatas | 205,767 µs; 93.210 B; 1.637 asignaciones | **19,810 µs; 15.882 B; 143 asignaciones** |
| 10.000 candidatas | 22,057 ms; 10.365.242 B; 160.054 asignaciones | **1,508 ms; 1.234.719 B; 10.043 asignaciones** |
| p95 / p99, 100 candidatas | 0,482 / 0,606 ms | **0,042 / 0,057 ms** |

La repetición nueva del binario original, sin perfiles, registró medianas de **17,436 / 197,679 µs** para 10/100 candidatas; confirma la dirección de la mejora sin depender solo de la lectura histórica. Se conserva [esa repetición](before/throughput-repeat.txt).

Frente a T3.6, que tenía 10,212 / 127,339 µs para 10/100 candidatas sin límites runtime, la baseline original ya no presenta una regresión de tiempo ni de asignaciones. Esto resuelve el sobrecoste pendiente de ese escenario; no certifica cualquier mezcla de reglas ni crea una garantía estadística portable a otra máquina.

Una referencia adicional, construida con el harness nuevo pero restaurando `evaluate.go` y `ranking.go` anteriores, permite comparar condiciones únicas: **15,898 / 197,356 µs / 26,671 ms** para 10/100/10.000. Conserva los límites y la compilación actuales; compara la ruta de evaluación y no pretende reconstruir la compilación histórica. [Datos de referencia](before/unique-reference.txt).

El control con condiciones distintas mide **14,425 / 124,077 µs / 17,121 ms** para 10/100/10.000 candidatas. Para 100 usa **64.461 B/op y 1.527 asignaciones/op**: el coste de los observadores CEL sigue presente cuando se deben evaluar expresiones diferentes.

RSS final: **31,3 MiB** con 10.000 condiciones repetidas y **94,6 MiB** de mediana con 10.000 únicas (tres procesos, **86,3–101,0 MiB**). Heap retenido único: 28,1 MiB. Las mediciones residentes siguen bajo 128 MiB; no son picos de compilación. Compilar 10.000 repetidas tarda 18,204 ms y 10.000 únicas 502,530 ms; sigue fuera de la ruta caliente.

El límite acumulado del caso adversarial sigue devolviendo `LIMIT_EXCEEDED`, pero pasa de 3,743 ms a **0,138 ms** porque contabiliza el coste reutilizado sin repetir la ejecución. El patrón regex excesivo se sigue cancelando antes de RE2, en **2,995 µs** de mediana. Los rechazos de contexto/decimal sobredimensionados siguen antes de CEL o de la conversión numérica.

Datos: [entorno y hashes](after/environment.txt), [throughput y control único](after/throughput.txt), [percentiles](after/latency.txt), [RSS repetido](after/resident.txt), [RSS único](after/unique-resident.txt), [adversariales](after/limits.txt). Se conservan también las mediciones y perfiles intermedios para justificar el segundo cambio, sin confundirlos con el resultado final.

## Validación y reproducción

Pasan `go test ./...`, `go test -race ./...`, `go vet ./...` y las tres campañas de fuzzing de 10 s con dos workers. Las pruebas existentes cubren ranking, empates, trace determinista, mutación aislada de resultados, concurrencia y fallo sin fallback; la nueva prueba diferencial comprueba que reutilizar condiciones no cambia resultados ni presupuestos. El runner incorpora `BenchmarkEvaluateUnique` para evitar ocultar el coste de los casos no reutilizables.

```sh
bash scripts/benchmark.sh bin/t39-opt-final --container
mkdir -p bin/t39-opt-final/profiles
docker run --rm --network=none --read-only --cpus=1 --memory=512m --memory-swap=512m \
  -e GOMAXPROCS=1 -e GOMEMLIMIT=448MiB \
  -v "$PWD/bin/t39-opt-final/profiles:/profiles" rulefare-benchmark:local \
  -test.run '^$' -test.bench '^BenchmarkEvaluate$/^100$' -test.benchtime=5s \
  -test.cpuprofile=/profiles/cpu.prof -test.memprofile=/profiles/memory.prof

go run cmd/pprof -top bin/t39-opt-final/profiles/cpu.prof
go run cmd/pprof -top -sample_index=alloc_space bin/t39-opt-final/profiles/memory.prof
```

Los archivos `.pprof` son las capturas originales renombradas para incluirlas fuera de `.gitignore`. `alloc_space` incluye calibración y preparación del harness; sus bytes totales no son B/op. Se conservan SHA256 de los binarios y hashes de fuentes; el árbol sigue sin commit. La publicación remota de CI no se ha verificado. F1 conserva como pendientes corpus aprobado, documentación integral y evidencia de CI/CPU en reposo/I/O.
