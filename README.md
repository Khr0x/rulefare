# rulefare

Rules behind every travel transaction.

> **Estado al 2026-09-22:** F1 (motor de reglas) completada técnicamente: corpus de 32 casos revisado y CI de `main` en verde para `09ff1e2`. La aceptación de tarifas reales pertenece a F0 y a los módulos comerciales. El motor de cálculo, API, persistencia, multi-tenancy, UI, importaciones, ledger y conciliación aún no existen.

## Estado actual

| Área | Estado verificable |
|---|---|
| Contratos, validación y compilación CEL | Implementados |
| Vigencia, scope, evaluación CEL y `NO_MATCH` | Implementados |
| Ranking por especificidad, prioridad y empate bloqueante | Implementados; el ID solo estabiliza la presentación |
| Validación de `Layer`, `EffectiveAt` y `Context` (T2.7) | Implementada antes de cualquier regla; errores tipados y normalización |
| Copia profunda del outcome devuelto (T2.8) | Implementada y probada |
| Trace (T2.5) | Especificidad, desempates y errores CEL sanitizados; pruebas locales de estabilidad y privacidad |
| CLI `rules validate/evaluate` (T3.1/T3.2) | Implementada y probada sin servicios externos |
| Evaluación concurrente (T3.3) | Pruebas dedicadas de determinismo y aislamiento pasan con `-race` |
| Fuzzing de rulesets, CEL/scopes y contextos (T3.4) | Tres targets con semillas versionadas; campañas locales sin fallos y ejecución breve configurada en CI |
| Benchmarks y perfiles (T3.5/T3.6/T3.9) | Límites activos; regresión de la baseline de evaluación corregida, control de condiciones únicas medido |
| Documentación de API, CLI, errores y límites (T3.8) | [Guía del paquete](./engine/README.md), contratos y ejemplos verificados localmente |
| CPU en reposo e I/O | [Medición local de sistema](./propuesta/mvp/benchmarks/2026-09-22/system/README.md): mediana 0,0027 % CPU; sin I/O de archivos/red en 4.100 evaluaciones observadas |
| Límites runtime y payload (T3.9) | Implementados y medidos; agotamiento de recursos bloquea toda la evaluación sin fallback |
| Corpus técnico (T1.6) | [32 casos revisados](./engine/testdata/README.md) por un agente independiente con enfoque financiero; snapshots y orden invertido; aceptación de tarifas reales separada |
| F2 · plataforma | 🟡 En curso: `rulefare serve` con config por entorno y apagado ordenado (T1.1); frontera de dependencias como prueba Go (T1.2); PostgreSQL con migraciones embebidas y advisory lock (T1.3); logs de acceso, `X-Request-ID`, `traceparent` y recuperación de pánicos (T1.4). Sin rutas ni UI todavía |
| F3–F10 del MVP | Pendientes |
| Verificación F1 | Build, suite, carrera, fuzzing, benchmark, CPU/I/O y `govulncheck` pasan en [CI de `main`](https://github.com/Khr0x/rulefare/actions/runs/35778045359); [artefacto de mediciones](https://github.com/Khr0x/rulefare/actions/runs/35778045359#artifacts) |

El motor, la documentación y el workflow ya están versionados. El [cierre técnico de F1](./propuesta/mvp/verificacion/2026-09-22/README.md#cierre-técnico-de-f1) se apoya en el CI del commit de merge `09ff1e2`; los cortes anteriores quedan como historial.

## Verificación local

Requiere Go 1.27 o posterior. Use la última revisión de parche disponible.

```sh
go build ./...
gofmt -l engine cmd
go test ./...
go test -race ./...
go vet ./...
govulncheck ./...
```

Los resultados y bloqueos vigentes se registran en el [plan de F1](./propuesta/mvp/f1_motor_reglas_plan_implementacion.md#estado-verificado-al-2026-09-22).

## Fuzzing

Las semillas de `engine/fuzz_test.go` se ejecutan con `go test ./...` y `go test -race ./...`. Para generar entradas nuevas, ejecutar cada target por separado:

```sh
go test ./engine -run '^$' -fuzz '^FuzzCompileRuleset$' -fuzztime=10s -parallel=2 -timeout=2m
go test ./engine -run '^$' -fuzz '^FuzzRuleCondition$' -fuzztime=10s -parallel=2 -timeout=2m
go test ./engine -run '^$' -fuzz '^FuzzEvaluateContext$' -fuzztime=10s -parallel=2 -timeout=2m
```

`FuzzCompileRuleset` parte de los fixtures válidos e inválidos y verifica compilación determinista, entrada inalterada y resolución independiente del orden de reglas. `FuzzRuleCondition` varía CEL y scopes sin depender de que una mutación conserve JSON válido. `FuzzEvaluateContext` explora entradas inesperadas y verifica estados/errores coherentes, trace sanitizado, determinismo y aislamiento al modificar resultados devueltos.

El harness limita sus entradas a 64 KiB para acotar las campañas; no es un límite de producción; los límites T3.9 se aplican dentro del motor. El CI configura 10 segundos por target con dos workers. Go guarda los fallos reproducibles en `engine/testdata/fuzz/`; esos casos deben conservarse como regresiones. La [ejecución remota del merge `09ff1e2`](https://github.com/Khr0x/rulefare/actions/runs/35778045359) pasó con los tres targets.

## Rendimiento

```sh
bash scripts/benchmark.sh bin/benchmarks-local
bash scripts/benchmark.sh bin/benchmarks-container --container
```

El runner mide throughput/asignaciones con 10, 100 y 10.000 candidatas, compilación de 10.000 reglas, p95/p99 de 100 candidatas y memoria en un proceso nuevo. Guarda métricas, entorno, commit, estado del árbol y hashes de fuentes. El modo contenedor usa Docker con 1 CPU y 512 MiB; el modo local solo fija `GOMAXPROCS` y el límite suave de Go.

La [baseline del 2026-09-22](./propuesta/mvp/benchmarks/2026-09-22/README.md) registra p95 **0,497 ms**, p99 **0,643 ms** y **47.598 B/op** para 100 candidatas en Linux arm64. El RSS original tras GC con 10.000 reglas fue **136,7 MiB**. El [perfilado T3.6](./propuesta/mvp/benchmarks/2026-09-22/t3.6/README.md) identificó programas CEL duplicados: reutilizar condiciones idénticas reduce el RSS a **29,5 MiB**, con p95/p99 de 0,409/0,435 ms. La [segunda optimización T3.6](./propuesta/mvp/benchmarks/2026-09-22/t3.6-unique/README.md) elimina funciones CEL no utilizadas de las tablas de evaluación y reduce el control de condiciones únicas a **86,4 MiB** de RSS mediano (tres procesos), desde la lectura histórica de 145,3 MiB. La latencia final p95/p99 es **0,417/0,434 ms**. T3.6 quedó completada y la [medición de CI del merge](https://github.com/Khr0x/rulefare/actions/runs/35778045359) conserva evidencia del corpus técnico de 32 casos. F1 está cerrada técnicamente; la aceptación de tarifas reales es separada.

La [primera medición T3.9](./propuesta/mvp/benchmarks/2026-09-22/t3.9/README.md) elevó la evaluación de 100 candidatas de 127 a 206 µs. La [optimización posterior](./propuesta/mvp/benchmarks/2026-09-22/t3.9-optimization/README.md), con todas las protecciones activas, reduce la baseline con condiciones repetidas a **19,8 µs**, **15.882 B/op** y **143 asignaciones/op**; p95/p99 **0,042/0,057 ms**. El control de 100 condiciones distintas registra **124,1 µs** y el RSS único mediano sigue en **94,6 MiB**. La regresión de la baseline original queda corregida; no se extrapola a cualquier mezcla de reglas.

El [CI del merge `09ff1e2`](https://github.com/Khr0x/rulefare/actions/runs/35778045359) ejecutó los benchmarks y conservó los resultados como artefacto por SHA. Los benchmarks no fallan automáticamente por rendimiento ni comparan plataformas distintas; el reporte distingue mediciones y cumplimiento del presupuesto.

## Estructura

```text
cmd/rulefare/ CLI con subcomandos rules y serve
internal/     plataforma F2: config (platform), servidor HTTP (httpapi) y PostgreSQL (postgres)
migrations/   esquema SQL versionado, embebido en el binario
scripts/      runner de benchmarks local/contenedor
engine/       motor neutral en Go + CEL, sin I/O
propuesta/    alcance, roadmap y arquitectura objetivo
.github/      CI propuesta
```

## API Go

La [guía del paquete `engine`](./engine/README.md) documenta compilación y evaluación, schema y reglas, outcomes, ranking, trace, errores tipados y códigos de validación. Incluye un ejemplo Go completo y enlaza los [límites de recursos](./engine/LIMITS.md).

## CLI

Compilar desde la raíz del repositorio y ejecutar los ejemplos incluidos:

```sh
go build -o bin/rulefare ./cmd/rulefare
./bin/rulefare rules validate \
  --schema engine/testdata/schema.json --ruleset engine/testdata/ruleset.json
./bin/rulefare rules validate \
  --schema engine/testdata/schema.json --ruleset engine/testdata/ruleset.json --json
./bin/rulefare rules evaluate \
  --schema engine/testdata/schema.json --ruleset engine/testdata/ruleset.json \
  --context engine/testdata/context.json --layer operation --at 2026-07-01T00:00:00Z
```

La evaluación de ejemplo devuelve `MATCH`, ganador `HOTELBEDS_MX` y outcome `percentage` con rate `0.17`, junto con el trace. `--context` recibe un objeto con las variables del schema, sin un envoltorio `context`. La fecha `--at` es obligatoria y acepta RFC3339 con zona horaria y fracciones de segundo.

| Comando | stdout | stderr |
|---|---|---|
| `rules validate` | Reporte legible; con `--json`, `ValidationReport` con `issues` | Errores de uso, lectura o decodificación |
| `rules evaluate` | `Result` JSON con outcome/trace, incluso ante `INVALID_INPUT`, `AMBIGUOUS_MATCH` o `LIMIT_EXCEEDED` | Reporte JSON si falla la compilación o el contexto; diagnóstico de recursos, ambigüedad, uso o I/O |

Los fallos previos a `Evaluate` no escriben un resultado en stdout. Los códigos de salida son `0` para éxito, incluyendo `NO_MATCH`; `1` para errores de archivos, JSON, compilación, evaluación o escritura; y `2` para uso incorrecto o fecha `--at` mal formada. `--help` y `-h` muestran ayuda tanto en la raíz como en los subcomandos.

Cada archivo admite hasta **8 MiB**, incluidos espacios. Se exige un único objeto JSON no nulo; el decoder usa `DisallowUnknownFields` para campos de estructuras y `UseNumber` para preservar enteros. Las variables del contexto se validan contra el schema en el motor. Se rechazan documentos adicionales y texto residual. Los errores de decodificación no reproducen valores del archivo.

Los comandos `rules` no requieren servicios externos ni inicializan infraestructura, ni leen variables `RULEFARE_*`.

### `rulefare serve`

Inicia la plataforma (F2): abre el puerto, se conecta a PostgreSQL, aplica las [migraciones pendientes](./migrations/README.md) y empieza a aceptar peticiones. Si la base no responde o una migración falla, sale con `1` antes de servir. Por ahora no registra rutas: responde `404` hasta que se añadan salud (T1.5) y endpoints (T2.4). Se configura solo por variables de entorno; valores inválidos detienen el arranque con código `1` y un log que nombra cada variable.

| Variable | Defecto | Uso |
|---|---|---|
| `RULEFARE_HTTP_ADDR` | `127.0.0.1:8080` | Dirección `host:puerto`; en contenedores, `:8080` |
| `RULEFARE_DATABASE_URL` | — (obligatoria) | Cadena de conexión PostgreSQL; nunca se escribe en logs |
| `RULEFARE_SHUTDOWN_TIMEOUT` | `15s` | Tiempo máximo para terminar peticiones en curso tras SIGINT/SIGTERM |

Los logs son JSON en stderr. Cada petición produce una línea `request` con `request_id`, `trace_id`, método, ruta sin query string, estado, bytes y duración; nunca cabeceras ni cuerpos. `X-Request-ID` se conserva si el cliente envía uno simple (hasta 128 caracteres `[A-Za-z0-9._-]`) y se devuelve siempre en la respuesta. Un `traceparent` W3C válido hace que el `trace_id` sea el del llamador. Un pánico en un handler se registra con su stack y responde `500` `application/problem+json` con código `INTERNAL_ERROR`, sin exponer el detalle. Tras SIGINT o SIGTERM deja de aceptar conexiones, espera las peticiones en curso y sale con `0`; si vence el plazo, cierra las conexiones y sale con `1`.

Con un PostgreSQL local de prueba (Docker Compose llega en T1.6):

```sh
docker run -d --rm --name rulefare-pg -p 127.0.0.1:55432:5432 \
  -e POSTGRES_USER=rulefare -e POSTGRES_PASSWORD=rulefare -e POSTGRES_DB=rulefare postgres:18
RULEFARE_DATABASE_URL='postgres://rulefare:rulefare@127.0.0.1:55432/rulefare?sslmode=disable' \
  ./bin/rulefare serve
```

Las pruebas de integración usan `RULEFARE_TEST_DATABASE_URL` (formato `postgres://`) y crean un esquema aislado por prueba. Sin esa variable se omiten en local; con `CI` definida fallan, para que la CI no pueda pasar omitiéndolas:

```sh
RULEFARE_TEST_DATABASE_URL='postgres://rulefare:rulefare@127.0.0.1:55432/rulefare?sslmode=disable' go test ./...
```

## Garantías y límites actuales

- `Compile` clona las entradas, valida la estructura y precompila CEL con límites de tamaño, nodos, anidamiento y recursión.
- `Evaluate` no realiza I/O. `TestEvaluateConcurrent` comparte un `Program` entre 16 goroutines y verifica resultados/traces idénticos y aislamiento de outcomes, traces y errores retornados. Pasa con `-race` y `GOMAXPROCS` 1 y 4; la [suite de CI en `main`](https://github.com/Khr0x/rulefare/actions/runs/35778045359) también pasó.
- Los contextos pueden compartirse para lectura, pero el consumidor no debe modificarlos mientras una evaluación los utiliza. Los resultados y errores devueltos son independientes y pueden modificarse.
- `Evaluate` valida toda la entrada antes de filtrar reglas o ejecutar CEL. Una entrada inválida devuelve `*InvalidEvaluationError` con `Report` ordenado, estado `INVALID_INPUT`, ningún ganador y candidatos vacíos; nunca activa un fallback.
- El outcome devuelto es una copia profunda: mutarlo no altera el programa ni evaluaciones posteriores.
- Un error runtime ordinario de CEL aparece en el trace solo como `rule_id` y `status: "CONDITION_ERROR"`. Se omite `reason`: no se copia el mensaje interno, que puede contener valores del contexto.
- [T3.9](./engine/LIMITS.md) limita coste CEL por condición/evaluación, patrones regex antes de ejecutarlos, strings, texto agregado, cardinalidad y precisión decimal. Agotar recursos devuelve `EvaluationLimitError`, status `LIMIT_EXCEEDED`, sin ganador ni trace parcial; no activa fallback. Son cotas de recursos, no un timeout duro.
- La CLI limita cada documento JSON a 8 MiB antes de decodificarlo y conserva los enteros con `UseNumber`. Este límite de archivos complementa los límites runtime y por valor del motor; los hosts Go deben acotar su propia decodificación.

## Contrato de evaluación (T2.7)

- `Layer` debe coincidir exactamente con una capa compilada; vacío, espacios o una capa desconocida son errores. Una capa desconocida ya no devuelve `NO_MATCH`.
- `EffectiveAt` es obligatorio, distinto de cero y dentro de los años 1–9999 en UTC. Se normaliza a UTC sin usar el reloj del sistema.
- `Context` debe ser un mapa no nulo con **todas** las variables declaradas en `Schema`, incluso si una regla no las usa. Se rechazan variables ausentes, nulas o desconocidas; no se inventan valores por defecto.
- `bool` y `string` exigen esos tipos Go exactos, sin conversiones desde texto o números.
- `int` acepta enteros Go con y sin signo representables en `int64`, `json.Number` con literal entero decimal representable en `int64`, y `float64` integral dentro de ±(2^53−1). Se normaliza a `int64`. Se rechazan fracciones, NaN, infinito, desbordamientos, strings numéricos y `float32`.
- Para conservar enteros grandes al leer JSON, usar `json.Decoder.UseNumber()`. Con `json.Number`, fracciones y notación exponencial se rechazan; no se redondean.
- `timestamp` acepta `time.Time` o texto RFC3339 con zona y fracciones de segundo. Debe ser distinto de cero y estar dentro de los años 1–9999 en UTC; se normaliza a `time.Time` UTC.
- La normalización crea un mapa nuevo; no modifica la entrada. El reporte usa códigos `REQUIRED`, `SCHEMA_MISMATCH` e `INVALID_RANGE`, rutas JSON y mensajes sin valores del contexto.

`NO_MATCH` queda reservado para una entrada válida sin reglas aplicables. Errores ordinarios de una expresión CEL sobre una entrada válida siguen produciendo `CONDITION_ERROR`, sin texto interno en `reason`; la regla se descarta y otra regla válida puede ganar. Una entrada inválida sigue fallando antes de CEL, sin fallback.

## Documentación canónica

Cuando dos documentos discrepen, prevalece este orden:

1. [Alcance v1](./propuesta/mvp/travel_commission_engine_alcance_v1.md): qué entra en cada versión.
2. [Roadmap del MVP](./propuesta/mvp/roadmap.md): orden, dependencias y estado de entrega.
3. [Plan de implementación de F1](./propuesta/mvp/f1_motor_reglas_plan_implementacion.md): trabajo técnico del motor.
4. [Jerarquía y resolución](./propuesta/mvp/travel_commission_engine_jerarquia_reglas.md): semántica de selección.

Los documentos en `propuesta/otras-fases/` describen arquitectura objetivo y opciones futuras; no son compromisos del MVP.

La [verificación técnica de F1 y preparación de CI](./propuesta/mvp/verificacion/2026-09-22/README.md) registra dependencias, pruebas de aceptación y escaneo local sin vulnerabilidades detectadas. CI incluye ahora CPU/I/O y sus artefactos; el run remoto verificado del commit base no cubre los cambios locales actuales.
