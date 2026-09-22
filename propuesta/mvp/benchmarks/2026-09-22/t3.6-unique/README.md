# T3.6 · Condiciones únicas dentro del presupuesto medido

La segunda optimización reduce el RSS tras GC de 10.000 condiciones distintas a **86,4 MiB de mediana**, con tres observaciones entre **86,2 y 88,3 MiB**, todas por debajo de 128 MiB. El caso que inicialmente registró 145,3 MiB queda resuelto en el escenario medido. T3.6 se completa localmente; esto no acredita todos los posibles rulesets ni cierra F1 sin el corpus aprobado y los demás criterios de salida.

## Causa y cambio mínimo

El [perfil anterior](before/retained-top.txt) identifica 30,10 MiB de heap muestreado en `defaultDispatcher.Add`, el 60,56 % de la muestra viva. CEL cargaba en cada programa las tablas de todas las funciones estándar, aunque la expresión usara pocas. El [perfil posterior](after/retained-top.txt) reduce esa atribución a unos 3 MiB. Las asignaciones durante compilación están en [antes](before/allocations-top.txt) y [después](after/allocations-top.txt).

`Compile` sigue analizando y comprobando tipos con el entorno CEL completo. A partir del AST ya comprobado, la planificación recibe únicamente las funciones llamadas, manteniendo **todas las sobrecargas** de cada una. Esto conserva la resolución dinámica y evita confundir funciones usadas en llamadas anidadas con las usadas solo en la raíz. Se comparten entornos de planificación por conjunto de funciones dentro de una compilación; también se mantiene la reutilización anterior de expresiones idénticas. No hay caché global, APIs privadas, dependencias nuevas ni GC forzado dentro del motor. Se conserva el adaptador, proveedor de tipos y las declaraciones de variables.

El [diff de producción](compile.diff) documenta el cambio respecto a la primera optimización de T3.6. No se cambian las copias de reglas ni el aislamiento de resultados.

## Medición comparable

Mismo host Apple M4, Linux arm64 en Docker Desktop 29.7.2, Go 1.27.1, CEL v0.30.0, imagen base `scratch`, cuota 1 CPU, límite duro 512 MiB, sin swap/red, `GOMAXPROCS=1`, `GOMEMLIMIT=448MiB`. Cada medición de RSS usa un proceso nuevo y mantiene vivo el programa hasta terminar. Los perfiles se capturaron por separado. La corrida final se hizo después de terminar el fuzzing, para evitar esa competencia por recursos del host.

| 10.000 condiciones distintas | Antes, tres procesos | Después, tres procesos |
|---|---:|---:|
| RSS tras GC, mediana | 136,7 MiB | **86,4 MiB** |
| Rango de RSS tras GC | 136,7–138,9 MiB | **86,2–88,3 MiB** |
| Incremento de heap retenido, mediana | 54,4 MiB | **23,0 MiB** |

La lectura histórica de 145,3 MiB de la [primera entrega](../t3.6/README.md) permanece conservada. Las tres nuevas lecturas del binario anterior muestran la variación del RSS entre procesos; todas superan 128 MiB. Se informa la comparación nueva en vez de escoger aquella lectura mayor. RSS incluye ejecutable, runtime y páginas sin objetos vivos; los perfiles son muestras y no equivalen a `MemStats` ni al pico de memoria durante compilación. `rss-released-B` se conserva solo como diagnóstico y no se usa para acreditar el presupuesto.

Datos: [RSS anterior](before/unique-resident.txt), [RSS posterior](after/unique-resident.txt), [entorno y hashes posteriores](after/environment.txt). Se conservan los SHA256 de ambos binarios. El anterior incluye la primera optimización T3.6 y el perfilador configurado para condiciones únicas; el posterior corresponde a los hashes registrados. El diff permite reconstruir la versión anterior de producción. No se compara `ns/op` del benchmark de RSS como latencia de compilación: incluye observación y GC.

## Resto de los benchmarks

Medianas de tres repeticiones, salvo RSS repetido (una ejecución nueva). Las asignaciones corresponden al benchmark sin perfiles activos.

| Métrica posterior | Resultado |
|---|---:|
| Evaluar 10 candidatas | 10,212 µs; 6.664 B/op; 74 asignaciones/op |
| Evaluar 100 candidatas | 127,339 µs; 47.598 B/op; 437 asignaciones/op |
| Evaluar 10.000 candidatas | 8,369 ms; 5.804.974 B/op; 40.050 asignaciones/op |
| Compilar 10.000 condiciones repetidas | 14,521 ms; 15.844.117 B/op; 300.527 asignaciones/op |
| Compilar 10.000 condiciones distintas | 429,322 ms; 301.409.936 B/op; 5.469.598 asignaciones/op |
| p95 / p99 de evaluación, 100 candidatas | **0,417 / 0,434 ms** |
| RSS tras GC, 10.000 condiciones repetidas | **31,4 MiB** |

La latencia sigue dentro de los objetivos de 5/10 ms. Frente a la primera optimización T3.6, evaluar 100 candidatas pasa de 126,246 a 127,339 µs y mantiene sus asignaciones; compilar condiciones repetidas pasa de 13,651 a 14,521 ms. Estas mediciones no muestran una regresión de tiempo superior al 10 % en esos casos. La comparación usa el mismo host y límites, sin constituir una garantía estadística ni una baseline portable a otro hardware.

Datos: [throughput](after/throughput.txt), [percentiles](after/latency.txt), [RSS repetido](after/resident.txt). El runner ahora también mide compilación de condiciones únicas y tres procesos para su RSS; el artefacto `*.txt` existente en CI incluye esos resultados. La ejecución remota sigue sin verificar.

## Reproducir y validar

```sh
bash scripts/benchmark.sh bin/t36-unique-after-final --container
mkdir -p bin/t36-unique-after-final/profiles
docker run --rm --network=none --read-only --cpus=1 --memory=512m --memory-swap=512m \
  -e GOMAXPROCS=1 -e GOMEMLIMIT=448MiB \
  -e RULEFARE_PROFILE_DIR=/profiles -e RULEFARE_PROFILE_UNIQUE=1 \
  -v "$PWD/bin/t36-unique-after-final/profiles:/profiles" \
  rulefare-benchmark:local -test.run '^TestCompileMemoryProfile$' -test.v

go run cmd/pprof -top -sample_index=inuse_space bin/t36-unique-after-final/profiles/retained.prof
go run cmd/pprof -top -sample_index=alloc_space bin/t36-unique-after-final/profiles/allocations.prof
```

Se conservan cuatro perfiles por fase (`before`, `compiled`, `retained`, `allocations`) con extensión `.pprof` para no excluirlos mediante `.gitignore`. `compiled` es una muestra al terminar, sin GC explícito; `allocations` incluye las asignaciones durante compilación y la preparación/diagnóstico. Ninguno mide un pico exacto.

Pasan `go test ./...`, `go test -race ./...` y `go vet ./...`, además de tres campañas de fuzzing de 10 s por target y dos workers. La nueva prueba diferencial compara con CEL completo 16 expresiones sobre contextos alternantes: sobrecargas dinámicas, ternarios, listas/mapas, strings/regex, aritmética, conversiones, timestamps/duration, constantes de tipo y errores runtime. Se ejecuta también bajo el detector de carreras, junto con las pruebas concurrentes del motor.
