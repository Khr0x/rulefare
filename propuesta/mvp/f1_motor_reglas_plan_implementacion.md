# F1 · Plan de implementación del motor de reglas

> Motor nativo Go, independiente, determinista y sin I/O

| Campo | Valor |
|---|---|
| Versión | 0.2 |
| Última actualización | 2026-09-04 |
| Estado | 🟡 En curso · Semana 1 reabierta; Semana 2 parcial |
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
- CLI `rulefare-rules` para validar y evaluar archivos JSON.
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
├── evaluate.go       # ruta caliente
├── ranking.go        # comparación lexicográfica
├── trace.go          # trace y reason codes
├── errors.go         # errores tipados
├── engine_test.go
├── fuzz_test.go
├── benchmark_test.go
└── testdata/
    ├── ruleset.json
    ├── schema.json
    └── cases.json

cmd/rulefare-rules/
└── main.go
```

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
1. Verificar que Layer existe y que Context cumple Schema.
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
| `CONDITION_ERROR` | CEL falló en runtime |
| `SHADOWED` | Aplicaba, pero perdió por especificidad/prioridad |
| `WINNER` | Regla seleccionada |
| `AMBIGUOUS` | Empate bloqueante |

El trace registra IDs, versión, perfil de especificidad y desempates. No copia todo el contexto ni datos sensibles. Los errores internos de CEL deben convertirse en reason codes estables; no se expone directamente `error.Error()`.

## 9. Plan de trabajo

### Semana 1 · Contrato, validación y compilación

**Periodo:** 2026-09-14 → 2026-09-18  
**Hito:** un ruleset válido compila a un `Program` inmutable.  
**Estado:** 🟡 Implementada en su mayor parte; reabierta por la mutabilidad observable del outcome y por falta de evidencia vigente en CI.

| ID | Trabajo | Entregable | Estado |
|---|---|---|---|
| T1.1 | Inicializar módulo Go, `engine/` y CI mínima | `go test ./...` ejecutable | ✅ |
| T1.2 | Cerrar contrato de schema, ruleset, outcomes, resultado y errores | `types.go`, ejemplos JSON | ✅ |
| T1.3 | Implementar validación estructural acumulativa | `ValidationReport` | ✅ |
| T1.4 | Integrar CEL con schema estricto y retorno booleano | `compile.go` | ✅ |
| T1.5 | Garantizar inmutabilidad y agrupación por capa | `Program` compilado | 🟡 Parcial: compilar clona la entrada, pero el outcome devuelto comparte memoria |
| T1.6 | Crear corpus dorado inicial desde los ejemplos de la propuesta | `engine/testdata` | 🟡 Parcial: hay fixtures técnicos; falta `cases.json` y aprobación de producto/finanzas |

### Semana 2 · Evaluación, ranking y trace

**Periodo:** 2026-09-21 → 2026-09-25  
**Hito:** `Evaluate` resuelve `FIRST_MATCH` de forma determinista.

| ID | Trabajo | Entregable | Estado |
|---|---|---|---|
| T2.1 | Implementar vigencia y match exacto de scope | filtros con pruebas de borde | ✅ |
| T2.2 | Evaluar programas CEL precompilados | ruta caliente sin compilación | 🟦 Implementada; suite actual no compila |
| T2.3 | Implementar vector y comparador lexicográfico | `ranking.go` | ⬜ |
| T2.4 | Implementar prioridad y error de empate | `AMBIGUOUS_MATCH` | ⬜ |
| T2.5 | Implementar `NO_MATCH` y `EvaluationTrace` estable | `trace.go` | 🟡 Parcial: `NO_MATCH` y trace básico existen; faltan especificidad, desempates y ambigüedad |
| T2.6 | Cubrir percentage, fixed y tiered como outcomes tipados | validación + round-trip JSON | ✅ |
| T2.7 | Validar y normalizar layer, fecha y contexto contra el schema | error tipado antes de CEL | ⬜ |
| T2.8 | Desacoplar profundamente el resultado del programa compilado | outcome clonado + prueba de mutación | ⬜ |

### Semana 3 · CLI, seguridad concurrente y rendimiento

**Periodo:** 2026-09-28 → 2026-10-02  
**Hito:** artefacto standalone validado dentro del presupuesto de F1.

| ID | Trabajo | Entregable | Estado |
|---|---|---|---|
| T3.1 | Crear `rulefare-rules validate` | reporte legible y JSON | ⬜ |
| T3.2 | Crear `rulefare-rules evaluate` | resultado + trace JSON | ⬜ |
| T3.3 | Ejecutar race detector y pruebas concurrentes | evidencia `go test -race` | ⬜ |
| T3.4 | Añadir fuzzing de ruleset/contexto | seeds y fuzz targets | ⬜ |
| T3.5 | Medir latencia, memoria y asignaciones | baseline reproducible | ⬜ |
| T3.6 | Perfilar y optimizar solo incumplimientos medidos | perfiles antes/después | ⬜ |
| T3.7 | Bloquear imports internos e infraestructura en CI | prueba de arquitectura | ✅ Adelantada |
| T3.8 | Documentar API, CLI, errores y límites | README del paquete | ⬜ |
| T3.9 | Limitar coste runtime CEL y tamaños de valores/payload | límites medidos + pruebas adversariales | ⬜ |
| T3.10 | Endurecer supply chain de CI | `govulncheck`, Actions por SHA y Go con parche vigente | ⬜ |

## Estado verificado al 2026-09-04

La evidencia siguiente corresponde al árbol local auditado con Go 1.27.1. En `HEAD` solo está versionado el `README.md`; el motor, workflow y documentación siguen sin seguimiento, por lo que todavía no existe evidencia remota de CI.

| Comprobación | Resultado actual |
|---|---|
| `go build ./...` | ✅ Pasa |
| `gofmt -l engine` | ❌ `engine/engine_test.go` no está formateado |
| `go test ./...` | ❌ No compila: `engine/engine_test.go:585` usa `cel.Program` sin importar `github.com/google/cel-go/cel` |
| `go test -race ./...` y `go vet ./...` | ❌ Bloqueados por el mismo error |
| `go mod tidy -diff` | ✅ Sin diferencias |
| `govulncheck ./...` | ✅ No reporta vulnerabilidades alcanzables al 2026-09-04 |

En una copia temporal se corrigió únicamente el import faltante y se aplicó `gofmt`. Sin cambiar el repositorio, pasaron la suite existente, `go test -race`, `go vet` y 20 repeticiones con orden aleatorio; la cobertura fue 91,2 %. Una prueba concurrente diagnóstica de solo lectura también pasó con el detector de carreras. En cambio, una prueba que muta el outcome devuelto demostró que esa mutación cambia evaluaciones posteriores del mismo `Program`.

### Rendimiento exploratorio

Medición local no versionada: Apple M4, Darwin arm64, Go 1.27.1 y `GOMAXPROCS=1`. Sirve para orientar, no para cerrar T3.5 ni comparar con el entorno objetivo de 1 vCPU.

| Escenario | Resultado observado |
|---|---:|
| Evaluación de 100 candidatas que hacen match | 18,4–19,6 µs/op; p95 51–53 µs; p99 61–63 µs; 37.828 B/op |
| Evaluación de 10.000 candidatas que hacen match | 3,20–3,85 ms/op; 4.917.508 B/op |
| Compilación de 10.000 reglas | 231–235 ms; ~201 MiB de asignación transitoria |
| Heap adicional retenido por 10.000 reglas compiladas | ~42,9 MiB |

La ruta de 100 candidatas queda holgadamente bajo el objetivo local, pero evaluar 10.000 candidatas asigna casi 5 MiB por llamada. Antes de congelar la API debe decidirse cómo el host entrega el subconjunto prefiltrado sin recompilar CEL; no hace falta añadir caché ni infraestructura distribuida.

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
go test ./engine/... ./cmd/rulefare-rules/...
go test -race ./engine/...
go test -fuzz=Fuzz -run=^$ ./engine/...
GOMAXPROCS=1 GOMEMLIMIT=512MiB go test -bench=. -benchmem ./engine/...
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

- [ ] `go test ./...` y `go test -race ./engine/...` pasan.
- [ ] CLI valida y evalúa los archivos de `testdata` sin servicios externos.
- [ ] La única dependencia externa del paquete es CEL.
- [ ] `engine` no importa ningún paquete `internal` del proyecto.
- [ ] `Program` es inmutable y seguro para concurrencia.
- [ ] Mutar un `Result` no cambia el `Program` ni otra evaluación.
- [ ] Layer, fecha o contexto inválidos fallan antes de resolver reglas; nunca activan un fallback.
- [ ] Ranking, prioridad, empate, vigencia y `NO_MATCH` cumplen la especificación.
- [ ] El trace es estable, explicable y no contiene el contexto completo.
- [ ] Percentage, fixed y tiered hacen round-trip sin `float`.
- [ ] La frontera JSON rechaza campos desconocidos, documentos sobredimensionados y tipos no normalizables.
- [ ] CEL y los valores de entrada tienen límites runtime medidos y probados.
- [ ] Los benchmarks cumplen latencia, memoria, CPU e I/O.
- [ ] CI ejecuta detector de carreras y `govulncheck`, y referencia Actions de terceros por SHA completo.
- [ ] La API pública y los reason codes están documentados.
- [ ] No se implementó ningún elemento declarado fuera de F1.

## 14. Definition of Done

F1 se marca `✅ Completa` cuando:

1. Todos los criterios de aceptación tienen evidencia en CI.
2. El corpus dorado fue revisado por producto/finanzas.
3. Los benchmarks y perfiles base quedaron asociados al commit de entrega.
4. F2 puede importar `engine`, pero `engine` compila y funciona sin F2.
5. Existe una demostración reproducible:

```sh
rulefare-rules validate --schema schema.json --ruleset ruleset.json
rulefare-rules evaluate --schema schema.json --ruleset ruleset.json \
  --context context.json --layer operation --at 2026-09-10T00:00:00Z
```

La salida debe identificar la regla ganadora, el outcome y por qué se descartó cada alternativa.

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
