# T3.6 · Perfiles y reutilización de condiciones CEL

Registro de la primera optimización. La [segunda entrega](../t3.6-unique/README.md) resuelve el exceso del control de condiciones únicas; se conservan aquí los resultados históricos.

La carga de T3.5 retenía 10.000 programas CEL idénticos. `Compile` ahora reutiliza un programa por texto de condición dentro de una sola compilación. El RSS tras GC de esa carga baja de **136,7 a 29,5 MiB**. El control con 10.000 condiciones distintas aún registra **145,3 MiB**: el presupuesto general de memoria de F1 permanece abierto.

## Evidencia y cambio

Los perfiles se capturaron en procesos separados de los benchmarks, bajo Linux arm64, Docker Desktop 29.7.2, Go 1.27.1, CEL v0.30.0, 1 CPU, 512 MiB, sin swap/red, `GOMAXPROCS=1`, `GOMEMLIMIT=448MiB`. Se conserva la [baseline original](../README.md) sin sustituir sus resultados.

Antes del cambio, el perfil de heap vivo atribuye 50,61 MiB acumulados (89,24 % de la muestra) a `cel.newProgram`; 34,61 MiB corresponden a `defaultDispatcher.Add`, que construye las tablas de funciones de cada programa. `cloneRule` aporta 3,50 MiB. El perfil de asignaciones atribuye 62,65 MiB a esas tablas y también muestra el coste repetido del parser y del checker. La duplicación de programas es el primer objetivo justificado; las copias de reglas preservan el aislamiento del input.

El heap muestreado no equivale al RSS ni al incremento exacto de heap medido con `MemStats`. El RSS incluye ejecutable, runtime y páginas que ya no contienen objetos vivos. La baseline devolvía unos 32 MiB adicionales al forzar `FreeOSMemory`, señal de páginas recuperables; no se añadió GC ni devolución forzada de memoria al motor.

El mapa de reutilización vive solamente durante `Compile`, usa el texto exacto (salvo condiciones vacías, que ya se normalizaban a `true`) y almacena únicamente programas válidos. No hay caché global, resultados de evaluación compartidos ni reutilización entre schemas. Cada regla conserva scope, prioridad, outcome y trace independientes. Las condiciones inválidas siguen generando un error por cada ruta de regla.

## Comparación en el mismo entorno

Tres repeticiones de tiempo/asignaciones y percentiles; mediana en la tabla. Memoria: una observación por escenario, cada una en proceso nuevo, sin perfiles activos. Misma imagen base `scratch` y límites; la imagen cambia al cambiar el binario.

| Métrica | T3.5, antes | T3.6, después |
|---|---:|---:|
| RSS tras GC, 10.000 reglas | 136,7 MiB | **29,5 MiB** |
| Incremento de heap retenido | 53,5 MiB | 5,3 MiB |
| RSS tras devolución forzada, diagnóstico | 104,7 MiB | 20,4 MiB |
| Compilación 10.000 | 575,316 ms | 13,651 ms |
| Bytes por compilación | 363.478.756 | 15.857.805 |
| Asignaciones por compilación | 5.500.036 | 300.774 |
| Evaluación 10 | 10,600 µs | 10,047 µs |
| Evaluación 100 | 140,791 µs | 126,246 µs |
| Evaluación 10.000 | 21,212 ms | 8,881 ms |
| Bytes/asignaciones por evaluación 100 | 47.598 / 437 | 47.598 / 437 |
| Bytes/asignaciones por evaluación 10.000 | 5.804.811 / 40.048 | 5.804.977 / 40.050 |
| p95 / p99, 100 candidatas | 0,497 / 0,643 ms | 0,409 / 0,435 ms |

Datos posteriores: [entorno y hashes](after/environment.txt), [throughput](after/throughput.txt), [percentiles](after/latency.txt), [RSS](after/resident.txt). Los perfiles [antes](before/retained-top.txt) y [después](after/retained-top.txt) muestran la desaparición de las tablas CEL duplicadas entre los principales retenedores. Los perfiles de asignación incluyen preparación y diagnóstico; los B/op de la tabla provienen del benchmark sin instrumentación.

## Control sin condiciones repetidas

`BenchmarkProgramResidentUnique10000` cambia el umbral de `amount_minor` a un entero diferente por regla, conservando el resto de la estructura. [Resultado](after/unique-resident.txt): heap retenido 54,4 MiB, RSS tras GC **145,3 MiB**, tras devolución forzada 110,3 MiB. No tiene una medición anterior equivalente y no se presenta como regresión. Evita extrapolar el beneficio de la reutilización a reglas con condiciones distintas.

T3.6 deja una optimización medida y probada, pero sigue abierto el caso de condiciones únicas. Hace falta perfilar ese escenario antes de decidir un segundo cambio y validar el corpus aprobado antes de cerrar F1. CI remoto, CPU en reposo e I/O siguen pendientes.

## Reproducir

Desde la raíz del repositorio, con Docker activo y las dependencias Go disponibles:

```sh
bash scripts/benchmark.sh bin/t36-after --container
mkdir -p bin/t36-after/profiles
docker run --rm --network=none --read-only --cpus=1 --memory=512m --memory-swap=512m \
  -e GOMAXPROCS=1 -e GOMEMLIMIT=448MiB -e RULEFARE_PROFILE_DIR=/profiles \
  -v "$PWD/bin/t36-after/profiles:/profiles" \
  rulefare-benchmark:local -test.run '^TestCompileMemoryProfile$' -test.v

docker run --rm --network=none --read-only --cpus=1 --memory=512m --memory-swap=512m \
  -e GOMAXPROCS=1 -e GOMEMLIMIT=448MiB rulefare-benchmark:local \
  -test.run '^$' -test.bench '^BenchmarkProgramResidentUnique10000$' \
  -test.benchtime=1x -test.count=1

go run cmd/pprof -top -sample_index=inuse_space bin/t36-after/profiles/retained.prof
go run cmd/pprof -top -sample_index=alloc_space bin/t36-after/profiles/allocations.prof
```

Se usa `go run cmd/pprof` porque este toolchain local no instala `go tool pprof`. Con otro toolchain puede usarse ese comando habitual.

`TestCompileMemoryProfile` se omite salvo que se configure la variable de entorno. Guarda `before` antes de compilar, `compiled` inmediatamente después (sin GC explícito), `retained` tras GC y `allocations` con el historial de asignaciones, incluidas las ocurridas durante compilación. El heap de Go es muestreado y se actualiza por ciclos de GC: `compiled` no representa un pico exacto ni una fotografía completa de todos los temporales. Se mantiene vivo el programa hasta terminar las capturas. Los perfiles `.pprof` conservados aquí son los archivos originales `.prof` renombrados para no excluirlos con `.gitignore`; incluyen símbolos y pueden abrirse sin el binario para tablas de funciones.

Los perfiles anteriores se generaron con `compile.go` original de `6c86f4d`, coincidente con el hash de la baseline, y el nuevo test de perfilado; los posteriores corresponden a los hashes de `after/environment.txt`. Se conservan también los SHA256 de ambos binarios. Los binarios y capturas de trabajo quedan en `bin/`, ignorado por Git; no son artefactos de producción.

Validación: `go test ./...`, `go test -race ./...`, `go vet ./...`; pruebas de reutilización entre capas, cambios de contexto, aislamiento entre schemas y errores por regla. Se mantienen las pruebas concurrentes y de mutación de resultados de T3.3. Los tres targets de fuzzing pasaron campañas adicionales de 10 s por target, con dos workers y sin fallos.
