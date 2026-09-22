# F1 · Plan de implementación del motor de reglas

> Motor nativo Go, independiente, determinista y sin I/O

| Campo | Valor |
|---|---|
| Versión | 0.17 |
| Última actualización | 2026-09-22 |
| Estado | 🟡 En curso · Mediciones locales de sistema completadas; corpus y evidencia remota pendientes |
| Inicio objetivo | 2026-09-14 |
| Fin objetivo | 2026-10-02 |
| Duración | 3 semanas |
| Responsable | Backend / Tech Lead |
| Dependencias técnicas | Ninguna |
| Dependencia externa permitida | CEL para Go |

Este plan desarrolla la [F1 del roadmap](./roadmap.md#f1--motor-de-reglas-nativo-e-independiente). El comportamiento funcional se deriva de [Jerarquía y resolución](./travel_commission_engine_jerarquia_reglas.md) y el corte de producto de [Alcance v1](./travel_commission_engine_alcance_v1.md#bloque-2--reglas-reutilizables-sobre-productos).

## 1. Objetivo

Entregar un paquete Go importable y una CLI que puedan:

1. Compilar y validar un ruleset con condiciones CEL.
2. Evaluar un contexto sin acceder a red, disco o base de datos.
3. Elegir una regla con jerarquía lexicográfica y `FIRST_MATCH`.
4. Devolver un outcome tipado y un `EvaluationTrace` determinista.
5. Ejecutarse concurrentemente dentro de los presupuestos de latencia y memoria.

F1 **resuelve reglas**, pero no calcula comisiones monetarias. Por ejemplo, devuelve «aplicar 12 %» o una tabla tiered; el Calculation Engine de F5 aplica ese outcome sobre una base comisionable.

## 2. Entregables

- Paquete público `engine` dentro del módulo Go principal.
- Único ejecutable `rulefare`, con subcomandos `rules validate` y `rules evaluate` para archivos JSON.
- Contrato JSON versionado para schema, ruleset, contexto, resultado y trace.
- Suite unitaria, de carrera, fuzzing y corpus dorado.
- Benchmarks reproducibles con resultados base guardados en CI.
- Documento corto de uso, errores y garantías de concurrencia.

## 3. Alcance funcional

### Incluido

- Schema explícito y tipado del contexto.
- Capas identificadas por nombre, inicialmente `operation` y `split`.
- Scope exacto sobre dimensiones ordenadas.
- Condiciones CEL que deben producir booleano.
- Vigencia `valid_from` inclusiva y `valid_to` exclusiva.
- Versionado de ruleset.
- Outcomes `percentage`, `fixed` y `tiered` como datos tipados.
- Composición `FIRST_MATCH`.
- Prioridad solo para desempatar el mismo perfil de especificidad.
- Error bloqueante si el empate persiste.
- Resultado `NO_MATCH` si ninguna regla aplica.
- Trace estable de candidatos, descartes, ganador y desempates.

### Fuera de F1

- PostgreSQL, repositorios, HTTP, autenticación y multi-tenancy.
- Catálogo, contratos, bookings y tipos travel.
- Aplicación aritmética de outcomes sobre dinero.
- Persistencia y publicación de versiones.
- Importación Excel/CSV.
- `STACK`, splits a N partes y agregados persistentes.
- Conflict scan simbólico, simulación histórica y aprobaciones.
- Caché distribuida, plugins y motores alternativos a CEL.

## 4. Arquitectura interna

```text
                 JSON / structs Go
                       │
                       ▼
             ┌───────────────┐
             │ Validate      │  schema · ids · scope
             │ + Compile CEL │  vigencia · outcomes
             └───────┬───────┘
                     │ immutable Program
                     ▼
 Context + EffectiveAt + Layer
                     │
                     ▼
             ┌───────────────┐
             │ Evaluate      │
             │ 1. vigencia  │
             │ 2. scope     │
             │ 3. CEL       │
             │ 4. ranking   │
             │ 5. desempate │
             └───────┬───────┘
                     │
                     ▼
              Result + Trace
```

### Regla de dependencias

```text
engine → Go stdlib + CEL

engine ✗ internal/catalog
engine ✗ internal/postgres
engine ✗ internal/httpapi
engine ✗ organization_id / booking / supplier / product
```

El host debe entregar al motor las reglas candidatas. El prefiltro SQL descrito en la arquitectura general se implementará fuera de `engine` cuando exista persistencia. La API actual aún no permite seleccionar un subconjunto de un `Program` ya compilado: `Evaluate` recorre toda la capa. Esta decisión debe cerrarse antes de estabilizar el contrato para no obligar al host a recompilar CEL por petición.

### Estructura de archivos objetivo

```text
engine/
├── types.go          # contratos públicos
├── validate.go       # validación estructural
├── compile.go        # entorno y programas CEL
├── context.go        # validación y normalización de entrada
├── evaluate.go       # ruta caliente
├── ranking.go        # comparación lexicográfica
├── trace.go          # trace y reason codes
├── errors.go         # errores tipados
├── engine_test.go
├── concurrency_test.go
├── fuzz_test.go
├── benchmark_test.go
└── testdata/
    ├── ruleset.json
    ├── schema.json
    └── cases.json

cmd/rulefare/
└── main.go
```

### Decisión de CLI

`cmd/rulefare/` será la única entrada del ejecutable. F1 implementa `rulefare rules validate` y `rulefare rules evaluate`; F2 añadirá `rulefare serve` para iniciar la aplicación en el mismo binario. Los subcomandos `rules` no inicializan base de datos, HTTP, jobs ni frontend. La independencia del motor se mantiene en el paquete `engine`, sin dependencias hacia la plataforma.

No se crearán subpaquetes internos hasta que un archivo o dependencia real lo exija.

## 5. Contrato público propuesto

La forma exacta puede ajustarse durante T1.2, pero los conceptos y garantías son obligatorios.

```go
type RuleSet struct {
	ID         string
	Version    string
	Dimensions []string // menor → mayor rango
	Rules      []Rule
}

type Rule struct {
	ID        string
	Layer     string
	Scope     map[string]string
	Condition string
	ValidFrom *time.Time
	ValidTo   *time.Time
	Priority  int32
	Outcome   Outcome
}

type Schema struct {
	Variables map[string]ValueType
}

type Evaluation struct {
	Layer       string
	EffectiveAt time.Time
	Context     map[string]any
}

func Compile(schema Schema, rules RuleSet) (*Program, ValidationReport)
func (p *Program) Evaluate(input Evaluation) (Result, error)
```

### Decisiones del contrato

- `Program` debe ser profundamente inmutable después de compilar y seguro para concurrencia, incluidos los valores devueltos por `Evaluate`.
- No existe reloj global: `EffectiveAt` siempre llega en la entrada.
- `organization_id` no forma parte del contrato de seguridad del motor.
- Los nombres de capas y dimensiones son datos, no enums travel compilados.
- CEL recibe solo variables declaradas en `Schema`.
- `Layer`, `EffectiveAt` y `Context` se validan y normalizan contra el contrato antes de ejecutar una condición; los errores de contexto fallan cerrado.
- Los importes y porcentajes del outcome se serializan como decimales canónicos, nunca `float`.
- F1 valida el outcome y lo devuelve; F5 realiza la aritmética financiera.
- El orden del resultado y del trace es estable para permitir comparación byte a byte.

## 6. Algoritmo de evaluación

```text
1. Validar Layer, EffectiveAt y todo Context; normalizar según Schema.
   Ante entrada inválida, devolver INVALID_INPUT sin evaluar candidatos.
2. Recorrer las reglas precompiladas de esa capa.
3. Descartar reglas fuera de vigencia.
4. Comparar cada restricción de Scope con Context.
5. Evaluar CEL solo para las reglas que superaron Scope.
6. Construir vector de especificidad según Dimensions.
7. Ordenar matches por:
   a. vector lexicográfico descendente;
   b. Priority descendente cuando el vector es idéntico.
8. Si las dos primeras siguen empatadas, devolver AMBIGUOUS_MATCH.
9. Si no hay matches, devolver NO_MATCH.
10. Devolver ganador, outcome y trace determinista.
```

El desempate final nunca utiliza orden de carga, orden de mapa ni ID. El ID solo estabiliza la presentación del trace; no decide dinero.

## 7. Validaciones de compilación

`Compile` debe rechazar:

- ID o versión de ruleset vacíos.
- IDs de regla duplicados.
- Dimensiones vacías o repetidas.
- Scope sobre dimensiones desconocidas.
- Capas vacías.
- `valid_to <= valid_from`.
- Condiciones CEL que no compilan o no devuelven booleano.
- Variables CEL no declaradas en el schema.
- Outcome desconocido o incompleto.
- Porcentajes fuera del rango permitido.
- Tiers vacíos, desordenados o solapados.
- Importe fijo sin divisa.
- Uso de `STACK` en v1.
- Cardinalidades o valores que superen los límites documentados de tamaño, precisión o coste.

La compilación devuelve todos los problemas detectables en un `ValidationReport`; no obliga al usuario a corregirlos uno por uno.

## 8. EvaluationTrace

Cada candidato recibe un reason code estable:

| Código | Significado |
|---|---|
| `NOT_EFFECTIVE` | Fuera de vigencia |
| `SCOPE_MISMATCH` | Al menos una dimensión no coincide |
| `CONDITION_FALSE` | CEL devolvió falso |
| `CONDITION_ERROR` | CEL falló en runtime; solo ID y status, sin `reason` |
| `SHADOWED` | Aplicaba, pero perdió por especificidad/prioridad |
| `WINNER` | Regla seleccionada |
| `AMBIGUOUS` | Empate bloqueante |

El trace registra IDs, versión, perfil de especificidad y desempates. No copia todo el contexto ni datos sensibles. Los errores ordinarios de CEL se representan exclusivamente con el status estable `CONDITION_ERROR` y el ID de la regla. Se omite `reason`; no se expone `error.Error()` ni se intenta filtrar su texto. Con contexto válido, un error ordinario descarta esa regla y permite que otra gane; T2.7 bloquea las entradas inválidas antes de evaluar. T3.9 distingue agotamiento de recursos: aborta toda la evaluación con `LIMIT_EXCEEDED` y `EvaluationLimitError`, sin ganador ni trace parcial.

## 9. Plan de trabajo

### Semana 1 · Contrato, validación y compilación

**Periodo:** 2026-09-14 → 2026-09-18  
**Hito:** un ruleset válido compila a un `Program` inmutable.  
**Estado:** 🟡 Inmutabilidad corregida y verificada localmente; falta corpus aprobado y evidencia remota de CI.

| ID | Trabajo | Entregable | Estado |
|---|---|---|---|
| T1.1 | Inicializar módulo Go, `engine/` y CI mínima | `go test ./...` ejecutable | ✅ |
| T1.2 | Cerrar contrato de schema, ruleset, outcomes, resultado y errores | `types.go`, ejemplos JSON | ✅ |
| T1.3 | Implementar validación estructural acumulativa | `ValidationReport` | ✅ |
| T1.4 | Integrar CEL con schema estricto y retorno booleano | `compile.go` | ✅ |
| T1.5 | Garantizar inmutabilidad y agrupación por capa | `Program` compilado | ✅ Compilar clona la entrada y Evaluate clona el outcome; prueba local |
| T1.6 | Crear corpus dorado inicial desde los ejemplos de la propuesta | `engine/testdata` | 🟡 [Corpus inicial](../../engine/testdata/README.md) de diez casos y snapshots probado localmente; falta revisión/aprobación de producto y finanzas |

### Semana 2 · Evaluación, ranking y trace

**Periodo:** 2026-09-21 → 2026-09-25  
**Hito:** `Evaluate` resuelve `FIRST_MATCH` de forma determinista.

| ID | Trabajo | Entregable | Estado |
|---|---|---|---|
| T2.1 | Implementar vigencia y match exacto de scope | filtros con pruebas de borde | ✅ |
| T2.2 | Evaluar programas CEL precompilados | ruta caliente sin compilación | ✅ Implementada; suite local en verde |
| T2.3 | Implementar vector y comparador lexicográfico | `ranking.go` | ✅ Pruebas locales |
| T2.4 | Implementar prioridad y error de empate | `AMBIGUOUS_MATCH` | ✅ Pruebas locales |
| T2.5 | Implementar `NO_MATCH` y `EvaluationTrace` estable | Trace en `evaluate.go` | ✅ Incluye especificidad, desempates, ambigüedad y errores CEL sanitizados; pruebas locales |
| T2.6 | Cubrir percentage, fixed y tiered como outcomes tipados | validación + round-trip JSON | ✅ |
| T2.7 | Validar y normalizar layer, fecha y contexto contra el schema | `context.go`, `InvalidEvaluationError`, `INVALID_INPUT` | ✅ Pruebas locales de rechazo y normalización |
| T2.8 | Desacoplar profundamente el resultado del programa compilado | outcome clonado + prueba de mutación | ✅ Prueba local |

### Semana 3 · CLI, seguridad concurrente y rendimiento

**Periodo:** 2026-09-28 → 2026-10-02  
**Hito:** artefacto standalone validado dentro del presupuesto de F1.

| ID | Trabajo | Entregable | Estado |
|---|---|---|---|
| T3.1 | Crear `rulefare rules validate` | reporte legible y JSON | ✅ Implementada y probada localmente |
| T3.2 | Crear `rulefare rules evaluate` | resultado + trace JSON | ✅ Implementada y probada localmente |
| T3.3 | Ejecutar race detector y pruebas concurrentes | `concurrency_test.go` + evidencia `go test -race` | ✅ Pruebas dedicadas y suite local en verde; ejecución remota de CI no verificada |
| T3.4 | Añadir fuzzing de ruleset/contexto | `fuzz_test.go`, semillas y tres targets | ✅ Campañas locales sin fallos; campañas breves configuradas en CI |
| T3.5 | Medir latencia, memoria y asignaciones | `benchmark_test.go`, runner y baseline | ✅ Medida localmente y en contenedor 1 CPU/512 MiB; ejecución remota pendiente |
| T3.6 | Perfilar y optimizar solo incumplimientos medidos | perfiles antes/después | ✅ Perfiles y optimizaciones medidas: RSS 31,4 MiB repetidas / 86,4 MiB únicas; evidencia local |
| T3.7 | Bloquear imports internos e infraestructura en CI | prueba de arquitectura | ✅ Adelantada |
| T3.8 | Documentar API, CLI, errores y límites | README del paquete | ✅ [Guía del paquete](../../engine/README.md), CLI y límites documentados; ejemplos Go/CLI ejecutados localmente |
| T3.9 | Limitar coste runtime CEL y tamaños de valores/payload | límites medidos + pruebas adversariales | ✅ Límites implementados, pruebas adversariales y mediciones locales; sobrecoste documentado |
| T3.10 | Endurecer supply chain de CI | `govulncheck`, Actions por SHA y Go con parche vigente | 🟦 Workflow configurado; falta verificar ejecución remota y vigencia del parche |

## Estado verificado al 2026-09-22

Verificación del árbol local sobre `6c86f4d` más T2.7, sanitización del trace, CLI, concurrencia, fuzzing, benchmarks y esta actualización, con Go 1.27.1 en Darwin arm64. Motor, documentación y CI ya están versionados. Este corte sustituye el bloqueo local de compilación/formato del 2026-09-04; la ejecución remota de CI no se ha verificado.

| Comprobación | Resultado actual |
|---|---|
| `go build ./...` | ✅ Pasa |
| `gofmt -l engine cmd` y `git diff --check` | ✅ Sin incidencias |
| `go test ./...` | ✅ Pasa |
| `go test -race ./...` y `go vet ./...` | ✅ Pasan |
| `go mod tidy -diff` | ✅ Sin diferencias |
| `govulncheck ./...` | No reejecutado en este corte; configurado en CI |

La suite cubre ranking, prioridad, empate bloqueante y aislamiento del outcome. T2.7 añade errores tipados antes de evaluar candidatos, rechazo de variables ausentes/nulas/desconocidas, normalización de enteros y timestamps, límites numéricos, fechas fuera de rango, JSON estándar y `UseNumber`, entrada no modificada y reportes deterministas sin valores del contexto. La prueba de fallback comprueba que incluso una regla global incondicional no se ejecuta ante entrada inválida. `TestEvaluateSanitizesCELErrors` reproduce errores de conversión y división por cero con contextos válidos, verifica la ausencia del texto interno en el resultado JSON y su estabilidad ante valores de entrada distintos, con y sin fallback.

La CLI se verifica con los fixtures del motor, salidas legibles/JSON, `MATCH`, `NO_MATCH`, empates, entrada inválida, enteros grandes, errores de uso e I/O y rechazo de archivos mal formados, campos desconocidos, documentos adicionales o archivos mayores de 8 MiB. El binario compilado ejecutó la demostración: validación sin issues y ganador `HOTELBEDS_MX` con rate `0.17`. Las pruebas están incluidas en `go test ./...`, que ya ejecuta el workflow existente.

### Evidencia de concurrencia (T3.3)

`TestEvaluateConcurrent` usa un mismo `Program` desde 16 goroutines con inicio coordinado y 64 evaluaciones por goroutine, en dos modos: solo lectura y mutación de resultados/errores propios. Cada modo compara 1.024 evaluaciones concurrentes y una comprobación final por caso contra una referencia serial compilada por separado, incluyendo JSON byte a byte y errores tipados. El programa compartido no se evalúa antes de iniciar las goroutines.

Los ocho casos cubren percentage, fixed, tiered, `NO_MATCH`, `AMBIGUOUS_MATCH`, contexto inválido, capa desconocida y `CONDITION_ERROR`. Las mutaciones alcanzan payloads de outcomes, límites de tiers, candidatos, vectores de especificidad, desempates e informes de error. Solo se modifican valores devueltos; los contextos compartidos se mantienen de solo lectura.

Pasó `go test -race ./engine -run '^TestEvaluateConcurrent$' -count=3 -cpu=1,4`, además de la suite completa con `-race` y `go vet`. El workflow existente ya incluye estas pruebas mediante `go test -race ./...`; aún no se verificó una ejecución remota. Estas pruebas verifican concurrencia y aislamiento, no sustituyen los benchmarks de T3.5.

### Evidencia de fuzzing (T3.4)

`engine/fuzz_test.go` contiene tres targets y 51 semillas iniciales, construidas desde los fixtures o declaradas en código:

- `FuzzCompileRuleset`: JSON de rulesets válidos, inválidos o malformados; verifica consistencia del reporte, ausencia de mutación y resolución/trace independientes del orden de reglas cuando compilan.
- `FuzzRuleCondition`: expresiones CEL y dimensiones/valores de scope, para ejercitar el compilador sin depender de la validez del JSON mutado.
- `FuzzEvaluateContext`: fechas, capas y contextos serializados, incluyendo límites enteros, valores nulos y tipos inesperados; verifica coherencia de estados/errores, entrada inalterada, trace sin mensajes CEL y aislamiento tras mutar outputs.

Se ejecutaron campañas exploratorias de 20 segundos por target y una comprobación final de 10 segundos por target con dos workers, sin fallos. La suite normal y el detector de carreras ejecutan las semillas. El workflow incorpora campañas de 10 segundos por target; no se ha verificado su ejecución remota.

Los documentos o argumentos generados se limitan a 64 KiB en el harness. El JSON malformado se descarta antes de llamar al motor; los contextos usan `UseNumber`. Estas campañas no sustituyen el corpus aprobado por finanzas, los benchmarks ni los límites runtime de T3.9. Si Go encuentra un fallo, guardar el caso minimizado de `engine/testdata/fuzz/` como regresión.

### Contrato cerrado de T2.7

- `Layer` debe existir exactamente en el programa; no se corrigen espacios ni se interpreta una capa desconocida como `NO_MATCH`.
- `EffectiveAt` es obligatorio, no cero, dentro de los años 1–9999 en UTC, y se normaliza a UTC.
- Todas las variables del schema son obligatorias, aunque ninguna regla de la capa las use. No hay variables opcionales ni valores por defecto; se rechazan campos desconocidos y valores nulos.
- La normalización crea un contexto nuevo con `bool`, `string`, `int64` y `time.Time` UTC. Los formatos aceptados y los límites de precisión están en el [contrato del README](../../README.md#contrato-de-evaluación-t27).
- Una entrada inválida devuelve `*InvalidEvaluationError` con `ValidationReport`, estado `INVALID_INPUT`, ganador nulo y candidatos vacíos. El reporte usa códigos/rutas estables sin valores del contexto.
- `NO_MATCH` solo describe entradas válidas sin regla aplicable. Los errores ordinarios de una expresión CEL con contexto válido mantienen `CONDITION_ERROR` sin texto interno en `reason`; la regla se descarta y el resto se evalúa normalmente.

### Rendimiento exploratorio histórico (2026-09-04)

Estos datos preceden al ranking y a T2.7; no miden la ruta actual. Medición local no versionada: Apple M4, Darwin arm64, Go 1.27.1 y `GOMAXPROCS=1`. Sirve para orientar, no para cerrar T3.5 ni comparar con el entorno objetivo de 1 vCPU.

| Escenario | Resultado observado |
|---|---:|
| Evaluación de 100 candidatas que hacen match | 18,4–19,6 µs/op; p95 51–53 µs; p99 61–63 µs; 37.828 B/op |
| Evaluación de 10.000 candidatas que hacen match | 3,20–3,85 ms/op; 4.917.508 B/op |
| Compilación de 10.000 reglas | 231–235 ms; ~201 MiB de asignación transitoria |
| Heap adicional retenido por 10.000 reglas compiladas | ~42,9 MiB |

En aquella medición, la ruta de 100 candidatas quedó bajo el objetivo local, pero evaluar 10.000 candidatas asignó casi 5 MiB por llamada. Antes de congelar la API debe decidirse cómo el host entrega el subconjunto prefiltrado sin recompilar CEL; no hace falta añadir caché ni infraestructura distribuida.

## 10. Estrategia de pruebas

| Grupo | Casos mínimos |
|---|---|
| Validación | IDs duplicados, dimensión desconocida, fechas inválidas, CEL no booleano, outcome inválido |
| Vigencia | antes de `valid_from`, exactamente en inicio, antes de fin, exactamente en fin, sin límites |
| Scope | global, una dimensión, varias, ausente en contexto, tipo incorrecto |
| Ranking | global vs country, supplier vs country, supplier+country vs supplier, contract sobre todos |
| Desempate | prioridad distinta, prioridad igual, orden de entrada aleatorio |
| CEL | true, false, error runtime, variable ausente, timestamp, enteros y strings |
| Outcomes | percentage, fixed, tiered, serialización determinista |
| Trace | razones completas, orden estable, sin copia de contexto sensible |
| Concurrencia | mismo `Program` desde múltiples goroutines con resultados idénticos |
| Fuzzing | JSON malformado, scopes arbitrarios, expresiones y contextos inesperados |
| Regresión | corpus dorado completo y snapshots de resultado/trace |

Comandos de verificación:

```sh
go test ./engine/... ./cmd/rulefare/...
go test -race ./engine/...
go test -race ./engine -run '^TestEvaluateConcurrent$' -count=3 -cpu=1,4
go test ./engine -run '^$' -fuzz '^FuzzCompileRuleset$' -fuzztime=10s -parallel=2 -timeout=2m
go test ./engine -run '^$' -fuzz '^FuzzRuleCondition$' -fuzztime=10s -parallel=2 -timeout=2m
go test ./engine -run '^$' -fuzz '^FuzzEvaluateContext$' -fuzztime=10s -parallel=2 -timeout=2m
bash scripts/benchmark.sh bin/benchmarks-local
bash scripts/benchmark.sh bin/benchmarks-container --container
```

## 11. Plan de rendimiento

Los benchmarks usan programas CEL ya compilados y contextos representativos.

| Escenario | Volumen | Presupuesto |
|---|---:|---:|
| Evaluación caliente pequeña | 10 candidatas | registrar baseline; sin regresión >10 % |
| Evaluación caliente objetivo | 100 candidatas | p95 ≤ 5 ms; p99 ≤ 10 ms |
| Compilación masiva | 10.000 reglas | medir tiempo; fuera de ruta caliente |
| Programa residente | 10.000 reglas | RSS ≤ 128 MiB |
| Evaluación | cualquier volumen | cero I/O |
| Reposo | programa cargado | CPU ≈ 0 |

El reporte debe registrar CPU, sistema operativo, versión de Go, versión de CEL y commit. Un cambio de entorno crea una baseline nueva; no se comparan números de máquinas distintas.

### Baseline vigente · T3.5 (2026-09-22)

El [reporte versionado](./benchmarks/2026-09-22/README.md) incluye resultados originales, entorno, commit base con cambios locales y hashes de fuentes. Se midió en Darwin arm64 y en un contenedor Linux arm64 con cuota de 1 CPU y límite duro de 512 MiB; los números de plataformas distintas no son comparables como regresiones.

- 100 candidatas, Linux controlado: mediana de throughput 140,791 µs/op, 47.598 B/op y 437 asignaciones/op. p95/p99 medianos: 0,497/0,643 ms; ambos dentro del objetivo, también en la peor repetición.
- Compilar 10.000 reglas: 575,316 ms y ~346,6 MiB de asignación total por operación, fuera de la ruta caliente.
- Programa residente: incremento de heap retenido ~53,5 MiB; RSS absoluto tras GC **136,7 MiB**, sobre 128 MiB. Tras `debug.FreeOSMemory`, RSS 104,7 MiB; ese diagnóstico no acredita el comportamiento normal.
- [T3.6](./benchmarks/2026-09-22/t3.6/README.md) identifica programas CEL duplicados y reutiliza condiciones idénticas por compilación: RSS tras GC **29,5 MiB**, heap retenido **5,3 MiB**, compilación **13,651 ms**, p95/p99 **0,409/0,435 ms**. Perfiles y mediciones antes/después conservados; pruebas, race detector y vet pasan.
- La [segunda optimización T3.6](./benchmarks/2026-09-22/t3.6-unique/README.md) conserva validación CEL completa y planifica con las funciones realmente llamadas. RSS mediano de 10.000 condiciones únicas: **86,4 MiB** (tres procesos entre 86,2 y 88,3 MiB), heap retenido **23,0 MiB**; RSS de condiciones repetidas **31,4 MiB**. Latencia final p95/p99 **0,417/0,434 ms**. Pruebas diferenciales, race detector, vet y fuzzing pasan.
- T3.6 queda completada localmente para ambos escenarios medidos. Queda validar el corpus aprobado, la ejecución remota y los demás criterios; en este corte aún faltaba la medición de sistema de CPU/I/O, aportada posteriormente abajo. F1 no se cierra.

`engine/benchmark_test.go` separa throughput/asignaciones, percentiles y residencia. El runner usa tres repeticiones para tiempos/percentiles y una ejecución en proceso nuevo para memoria. El CI configura el modo contenedor y conserva los archivos de resultados mediante una Action fijada por SHA; la ejecución remota aún no se verificó. No hay comparación automática del 10 % hasta disponer de una baseline y entorno CI comparables.

### Límites runtime medidos · T3.9 (2026-09-22)

[Contrato de límites](../../engine/LIMITS.md) y [reporte reproducible](./benchmarks/2026-09-22/t3.9/README.md). Coste CEL de 10.000 unidades por condición y 1.000.000 por evaluación; strings de 4 KiB, contexto textual de 64 KiB, compilación textual de 8 MiB, decimales de 38 dígitos/18 fraccionarios y regex de 256 bytes. Los excesos fallan sin fallback, con pruebas de límites, concurrencia, CLI y fuzzing.

La regex adversarial de 4 KiB pasaba 40,551 ms dentro del matcher antes de cancelar; el chequeo previo reduce el rechazo a 2,796 µs. Con los límites activos, p95/p99 son 0,482/0,606 ms y RSS único mediano 94,6 MiB. Se cumplen los objetivos absolutos de esos escenarios, pero evaluar 10/100 candidatas empeora ~67 %/~62 % respecto a T3.6, con más asignaciones por seguimiento de coste. La optimización posterior corrige la regresión de la baseline original sin eliminar protecciones (ver abajo). En ese corte seguían pendientes CPU en reposo, cero I/O, corpus aprobado y CI remoto; la medición de sistema posterior se documenta abajo.

### Optimización posterior de T3.9

[Perfiles y comparación](./benchmarks/2026-09-22/t3.9-optimization/README.md). Ranking mediante referencias y ordenación genérica, reserva del trace, inspección de errores fuera de la ruta correcta y reutilización de condiciones sin error dentro de una llamada. Cada regla conserva el consumo completo de presupuesto; no hay reutilización entre contextos ni cambios de límites.

La baseline de 10/100 candidatas queda en **5,986/19,810 µs**, frente a **10,212/127,339 µs** antes de T3.9 y **17,031/205,767 µs** con la primera protección. Para 100: **15.882 B/op**, **143 asignaciones/op**, p95/p99 **0,042/0,057 ms**. Control con 100 condiciones distintas: **124,077 µs**; las asignaciones del observador CEL persisten en ese escenario. RSS único mediano **94,6 MiB**. Pruebas diferenciales, adversariales, race, vet y fuzzing pasan. Se resuelve la regresión de evaluación del escenario original; F1 conserva los otros criterios pendientes.

### Medición de CPU en reposo e I/O (2026-09-22)

El [reporte de sistema](./benchmarks/2026-09-22/system/README.md) completa la evidencia local pendiente: tres procesos nuevos con 10.000 condiciones distintas registran CPU en reposo mediana **0,002676 %** durante ventanas de diez segundos. El trazado de 4.100 llamadas a `Evaluate` observa **cero accesos a archivos/red**, con controles positivos de apertura de archivo y creación de socket. Las reservas anónimas de memoria y esperas del runtime quedan identificadas por separado. Incluye la primera llamada, 100/10.000 candidatas, entradas inválidas y `NO_MATCH`; no prueba universalmente cualquier ruta o carga. Runner reproducible, suite Linux y vet pasan; no hay cambios de producción. F1 sigue abierta por corpus, evidencia remota y los demás criterios.

## 12. Riesgos y mitigaciones

| Riesgo | Señal temprana | Mitigación |
|---|---|---|
| Compilar CEL en la ruta caliente | latencia crece con cada evaluación | compilar una vez y hacer `Program` inmutable |
| Contexto demasiado dinámico | errores CEL en runtime | schema estricto al compilar y validar contexto antes de evaluar |
| Generalización excesiva | aparecen plugins, factories o DSL propio | limitar F1 a CEL, scope exacto y tres outcomes |
| Trace consume demasiada memoria | asignaciones dominan el perfil | reason codes y referencias a IDs; no copiar contexto |
| Resultados no deterministas | snapshots cambian entre ejecuciones | ordenar explícitamente; nunca depender de iteración de mapas |
| Data race en programas CEL | race detector falla | programas inmutables y datos por evaluación locales |
| Outcome devuelto comparte memoria | una mutación cambia evaluaciones futuras o activa el race detector | clonar el outcome al construir el resultado |
| Contexto inválido activa un fallback | aparece `CONDITION_ERROR` junto a un `MATCH` | validar/normalizar una vez y fallar cerrado antes de CEL |
| Coste CEL o payload sin cota | latencia/memoria crecen con strings o cardinalidades adversariales | `cel.CostLimit`, límites de bytes/precisión y corte temprano |
| JSON ambiguo o con typos | campos desconocidos o tipos `float64`/string llegan al motor | decoder estricto y normalización guiada por schema |
| Dependencia accidental de plataforma | import desde `internal/*` | comprobación de imports en CI |
| Supply chain mutable | Actions por tag o dependencias sin escaneo | SHA completo, parche Go vigente y `govulncheck` en CI |
| Presupuesto irreal para 10.000 reglas | RSS supera 128 MiB | perfilar; documentar capacidad medida antes de cambiar arquitectura |

## 13. Criterios de aceptación

- [x] `go test ./...` y `go test -race ./...` pasan localmente; evidencia remota pendiente.
- [x] CLI valida y evalúa los archivos de `testdata` sin servicios externos (pruebas locales y binario ejecutado).
- [x] La única dependencia externa directa de `engine` es CEL; sus dependencias transitivas permanecen en go.mod/go.sum (verificación local).
- [x] `engine` no importa paquetes `internal` ni otros paquetes del proyecto; control de CI reforzado y verificado localmente.
- [x] `Program` es inmutable y seguro para concurrencia con contextos de solo lectura (T3.3, pruebas locales con `-race`).
- [x] Mutar un `Result` no cambia el `Program` ni otra evaluación (prueba local).
- [x] Layer, fecha o contexto inválidos fallan antes de resolver reglas; nunca activan un fallback (T2.7, prueba local).
- [x] Ranking, prioridad, empate, vigencia y `NO_MATCH` cumplen la especificación en las pruebas técnicas locales; aprobación del corpus de negocio pendiente.
- [x] El trace es estable, explicable y no contiene el contexto completo ni mensajes internos de CEL (pruebas locales).
- [x] Percentage, fixed y tiered hacen round-trip sin `float` y se preservan en los snapshots de evaluación (pruebas locales).
- [x] La CLI rechaza campos desconocidos, archivos de más de 8 MiB y tipos no normalizables (pruebas locales).
- [x] CEL y los valores de entrada tienen límites runtime medidos y probados localmente (T3.9).
- [x] Los escenarios medidos localmente cumplen latencia, memoria, CPU e I/O; [evidencia de sistema](./benchmarks/2026-09-22/system/README.md). Pendientes corpus aprobado y verificación remota; no es una garantía para cualquier carga.
- [ ] CI ejecuta detector de carreras y `govulncheck`, y referencia Actions de terceros por SHA completo.
- [x] La API pública y los reason codes están documentados en [la guía del paquete](../../engine/README.md), con ejemplo Go y comandos CLI verificados localmente.
- [x] La revisión local de fuentes de producción mantiene el alcance de F1: engine y CLI, sin funcionalidades de fases posteriores.

La [verificación técnica y de CI](./verificacion/2026-09-22/README.md) conserva resultados y hashes. `govulncheck` no detecta vulnerabilidades localmente. Se encontró un run remoto exitoso del commit base del 6 de septiembre; no cubre los cambios actuales. El workflow ya incorpora CPU/I/O y sus artefactos, pendiente de ejecución remota del commit de entrega.

## 14. Definition of Done

F1 se marca `✅ Completa` cuando:

1. Todos los criterios de aceptación tienen evidencia en CI.
2. El corpus dorado fue revisado por producto/finanzas.
3. Los benchmarks y perfiles base quedaron asociados al commit de entrega.
4. F2 puede importar `engine`, pero `engine` compila y funciona sin F2.
5. Existe una demostración reproducible:

```sh
go build -o bin/rulefare ./cmd/rulefare
./bin/rulefare rules validate --schema engine/testdata/schema.json --ruleset engine/testdata/ruleset.json
./bin/rulefare rules evaluate --schema engine/testdata/schema.json --ruleset engine/testdata/ruleset.json \
  --context engine/testdata/context.json --layer operation --at 2026-07-01T00:00:00Z
```

La salida identifica `HOTELBEDS_MX` como ganadora, devuelve el outcome `percentage` con rate `0.17` y explica los descartes en el trace. El [README](../../README.md#cli) documenta flags, formatos, códigos de salida y límite de archivos.

## 15. Registro de seguimiento

| Fecha | ID | Estado anterior | Estado nuevo | Evidencia / bloqueo | Responsable |
|---|---|---|---|---|---|
| 2026-08-30 | F1 | — | ⬜ Planificada | Plan de implementación inicial | Tech Lead |
| 2026-08-30 | T1.1–T1.6 | ⬜ Pendientes | ✅ Completadas | Módulo, CI, contrato, validación, CEL precompilado, `Program` inmutable, corpus y pruebas locales en verde | Backend |
| 2026-08-30 | F1 | ⬜ Planificada | 🟡 En curso | Semana 1 cerrada; evaluación, ranking, CLI y rendimiento continúan pendientes | Backend |
| 2026-09-03 | T2.1 | ⬜ Pendiente | ✅ Completada | `engine/evaluate.go`: filtros de vigencia (from inclusivo / to exclusivo) y match exacto de scope + `Evaluate` provisional; `go test`, `go test -race` y `go vet` en verde | Backend |
| 2026-09-04 | T1.5 / T1.6 | ✅ Completadas | 🟡 Parciales | Outcome mutable desde el resultado; fixtures técnicos sin corpus `cases.json` aprobado | Backend / Producto |
| 2026-09-04 | T2.2 / T2.5 / T2.6 | ⬜ Pendientes | 🟦 / 🟡 / ✅ | CEL runtime y outcomes existen; trace sigue incompleto; suite actual no compila | Backend |
| 2026-09-04 | Verificación | Verde declarado | ❌ Roja | Build pasa; falta import de `cel` y `gofmt` en `engine_test.go`; entrada del 2026-09-03 queda como evidencia histórica | Backend |
| 2026-09-04 | Seguridad y escala | — | ⬜ Trabajo añadido | Fail-closed del contexto, copia de resultados, coste CEL, frontera JSON y supply chain incorporados | Backend / Tech Lead |
| 2026-09-22 | T1.5 / T2.2–T2.4 / T2.8 | Estados pendientes o parciales | ✅ Implementadas con pruebas locales | Código ya presente en `6c86f4d`: ranking, desempates y copia del outcome; suite actual en verde | Backend |
| 2026-09-22 | T2.7 | ⬜ Pendiente | ✅ Implementada con pruebas locales | `context.go`, error tipado antes de CEL, normalización JSON y regresiones sin fallback | Backend |
| 2026-09-22 | Verificación / T2.5 / T3 | Corte histórico rojo | Local verde; F1 sigue abierta | Build, suite, race, vet y tidy pasan; trace CEL, corpus, CLI, límites, fuzzing, benchmarks y evidencia remota pendientes | Backend |
| 2026-09-22 | T2.5 | 🟡 Sanitización pendiente | ✅ Implementada con pruebas locales | Eliminar `condErr.Error()` del trace; conversiones y división por cero exponen solo `CONDITION_ERROR`; salida JSON estable sin valores del contexto | Backend |
| 2026-09-22 | T3.1 / T3.2 | CLI separada del motor | Único ejecutable `rulefare` | Decisión acordada: `cmd/rulefare/`, subcomandos `rules validate/evaluate` y futura extensión `serve` en F2; implementación pendiente | Producto / Tech Lead |
| 2026-09-22 | T3.1 / T3.2 | ⬜ Pendientes | ✅ Implementadas con pruebas locales | CLI única con `flag`, validación legible/JSON, evaluación con trace, códigos de salida, límite de archivos y `engine/testdata/context.json`; binario ejecutado sin servicios | Backend |
| 2026-09-22 | T3.3 | 🟡 Suite con race sin pruebas dedicadas | ✅ Implementada y verificada localmente | `TestEvaluateConcurrent`: 16 goroutines, dos modos, ocho casos y mutación profunda de outputs; tres repeticiones con CPU 1/4 y suite completa con `-race`; CI remota pendiente | Backend |
| 2026-09-22 | T3.4 | ⬜ Pendiente | ✅ Implementada y verificada localmente | `FuzzCompileRuleset`, `FuzzRuleCondition` y `FuzzEvaluateContext`; semillas versionadas y campañas locales sin fallos; workflow ampliado con 10 s por target | Backend |
| 2026-09-22 | T3.5 | ⬜ Pendiente | ✅ Baseline implementada y medida | Evaluación 10/100/10.000, compilación 10.000, percentiles y RSS; datos versionados, runner y artefactos CI; exceso de RSS queda para T3.6 | Backend |
| 2026-09-22 | T3.6 | ⬜ Pendiente | 🟡 Optimización medida; presupuesto general abierto | Perfiles identifican duplicación CEL; reutilización local reduce RSS de baseline a 29,5 MiB; control único 145,3 MiB | Backend |
| 2026-09-22 | T3.6 | 🟡 Condiciones únicas sobre presupuesto | ✅ Completada localmente | Perfiles identifican tablas CEL no utilizadas; planificación por funciones usadas reduce RSS único a 86,4 MiB; pruebas diferenciales y benchmarks conservados | Backend |
| 2026-09-22 | T3.9 | 🟡 Límites runtime pendientes | ✅ Implementada y medida localmente | Cotas previas de payload/precisión, CostLimit individual y agregado, regex acotada antes de ejecutar, error bloqueante y pruebas adversariales; sobrecoste >10 % documentado | Backend |
| 2026-09-22 | Rendimiento T3.9 | Sobrecoste >10 % en baseline | ✅ Regresión del escenario original corregida | 100 candidatas repetidas 19,8 µs; únicas 124,1 µs; perfiles justifican cambios, presupuestos y resultados equivalentes | Backend |
| 2026-09-22 | T3.8 | 🟡 Documentación integral pendiente | ✅ Completada localmente | Guía de API, contratos, outcomes, ranking, trace y catálogo de códigos; CLI/límites enlazados y ejemplos Go/CLI ejecutados | Backend |
| 2026-09-22 | CPU en reposo / I/O | 🟡 Sin evidencia de sistema | ✅ Medidos localmente | CPU mediana 0,002676 %; cero accesos a archivos/red en 4.100 evaluaciones con controles positivos; corpus y CI remoto pendientes | Backend |
| 2026-09-22 | T1.6 | 🟡 Sin cases.json | 🟡 Corpus inicial listo para revisión | Diez casos explícitos, jerarquía canónica, snapshots y orden invertido; suite/race/vet locales pasan; aprobación de negocio pendiente | Backend / Producto / Finanzas |
| 2026-09-22 | Documentación de ranking | Guía indicaba primera dimensión | ✅ Corregida a última dimensión | Dimensions es menor → mayor rango; código y especificación ya coincidían, sin cambio de resolución | Backend |
| 2026-09-22 | Criterios técnicos / CI | Evidencia parcial | ✅ Verificación local; CI actual pendiente | Imports stdlib/CEL, round-trip/ranking, govulncheck y actionlint pasan; run antiguo verificado, medición de sistema añadida a CI | Backend |
