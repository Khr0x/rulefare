# API del motor de reglas

`github.com/Khr0x/rulefare/engine` compila reglas y selecciona un outcome. El paquete no calcula comisiones ni importes, no persiste resultados y no consulta el reloj, archivos, red o bases de datos al evaluar. El llamador aporta la fecha efectiva. Esta guía describe la implementación de F1; su cierre sigue sujeto al corpus aprobado y a la evidencia pendiente del [plan](../propuesta/mvp/f1_motor_reglas_plan_implementacion.md).

## Compilar una vez y evaluar

```go
package main

import (
    "fmt"
    "time"

    "github.com/Khr0x/rulefare/engine"
)

func main() {
    schema := engine.Schema{
        Version: "v1",
        Variables: map[string]engine.ValueType{"country": engine.ValueString},
    }
    rules := engine.RuleSet{
        ID: "COMMISSIONS", Version: "v1", SchemaVersion: "v1",
        Dimensions: []string{"country"},
        Rules: []engine.Rule{{
            ID: "MX", Layer: "operation",
            Scope: map[string]string{"country": "MX"},
            Outcome: engine.Outcome{
                Kind: engine.OutcomePercentage,
                Percentage: &engine.PercentageOutcome{Rate: "0.17"},
            },
        }},
    }
    program, report := engine.Compile(schema, rules)
    if !report.Valid() {
        fmt.Println(report.Issues)
        return
    }
    result, err := program.Evaluate(engine.Evaluation{
        Layer: "operation",
        EffectiveAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
        Context: map[string]any{"country": "MX"},
    })
    if err != nil {
        fmt.Println(result.Status, err)
        return
    }
    if result.Winner == nil { // NO_MATCH es un resultado válido.
        fmt.Println(result.Status)
        return
    }
    fmt.Println(result.Status, result.Winner.RuleID, result.Winner.Outcome.Percentage.Rate)
}
```

Salida: `MATCH MX 0.17`. Usar únicamente un `Program` obtenido de una compilación válida; su valor cero no es un programa compilado.

`Compile(Schema, RuleSet) (*Program, ValidationReport)` valida primero la estructura y luego CEL. Si existe cualquier issue de severidad `error`, devuelve programa nulo. `ValidationReport.Valid()` acepta reportes sin errores; `warning` está definido, pero la implementación actual solo emite errores. El reporte exitoso serializa `issues: []`.

La compilación copia schema, dimensiones, scopes, fechas y outcomes. Se pueden modificar las entradas después de que termine `Compile`; no durante la llamada. Un `Program` es inmutable y admite evaluaciones concurrentes. El llamador no debe modificar el contexto durante `Evaluate`. Los resultados y errores devueltos pertenecen al llamador y pueden modificarse sin afectar evaluaciones posteriores.

## Schema y reglas

Los nombres JSON y tipos públicos están en [types.go](types.go).

| Contrato | Campos y reglas |
|---|---|
| `Schema` | `version` no vacía; `variables` asocia nombres a `bool`, `int`, `string` o `timestamp`. Nombres: `[a-z][a-z0-9_]{0,63}`. |
| `RuleSet` | `id`, `version`, `schema_version`, `dimensions`, `rules`. `schema_version` debe ser idéntica a `Schema.Version`. |
| IDs | Ruleset y reglas usan `[A-Za-z][A-Za-z0-9._:-]{0,127}`. Los IDs de regla son únicos en todo el ruleset. |
| `dimensions` | Lista ordenada, sin duplicados, de variables `string` del schema. Se ordenan de menor a mayor rango: la última dimensión es la más importante para la especificidad. |
| `Rule` | `id`, `layer`, `outcome`; opcionales `scope`, `condition`, `composition`, `valid_from`, `valid_to`, `priority`. |
| `layer` | Texto no vacío ni compuesto solo de espacios. Las capas disponibles se derivan de las reglas; la evaluación compara el nombre exacto. |
| `scope` | Mapa de dimensión a string, con igualdad exacta y sensible a mayúsculas. Omitido o vacío es global. No hay comodines. |
| `condition` | CEL con resultado estático `bool`; omitida, vacía o solo espacios equivale a `true`. Las variables del schema están disponibles por nombre. Macros CEL deshabilitadas; no se registran funciones externas ni de I/O. |
| `composition` | Omitida se normaliza a `FIRST_MATCH`, único valor admitido. Significa un ganador por ranking, no la primera regla del archivo. |
| Vigencia | `valid_from` inclusivo y `valid_to` exclusivo; un límite omitido queda abierto. Si existen ambos, el final debe ser posterior al inicio. JSON usa timestamps con zona. |
| `priority` | Entero `int32`, cero por defecto; un número mayor gana solo entre perfiles de especificidad idénticos. |

Un ruleset vacío puede compilar, pero no tendrá una capa evaluable. La lista completa de cotas está en [LIMITS.md](LIMITS.md).

## Outcomes: datos, sin cálculo financiero

`Outcome` es una unión: exactamente un payload debe coincidir con `kind`.

| Kind | Ejemplo JSON | Validación |
|---|---|---|
| `percentage` | `{"kind":"percentage","percentage":{"rate":"0.17"}}` | Ratio entre 0 y 1, incluidos ambos extremos. |
| `fixed` | `{"kind":"fixed","fixed":{"amount":"25.5","currency":"USD"}}` | Importe no negativo; moneda con tres letras ASCII mayúsculas. No se consulta un catálogo de monedas. |
| `tiered` | `{"kind":"tiered","tiered":{"metric":"sales","tiers":[{"up_to_exclusive":100,"rate":"0.1"},{"up_to_exclusive":null,"rate":"0.2"}]}}` | `sales` debe ser variable `int`; al menos un tier. Límites estrictamente crecientes; únicamente el último es ilimitado y debe serlo. |

Rates e importes son strings decimales canónicos: `"0"`, `"1"`, `"0.17"`; no signos, exponentes, ceros iniciales innecesarios ni ceros fraccionarios finales (`"0.10"` es inválido). Máximo 38 dígitos, hasta 18 fraccionarios. El motor devuelve el payload ganador completo: no aplica porcentajes, no redondea, no convierte monedas ni selecciona/calcula un tier.

## Entrada de evaluación

`(*Program).Evaluate(Evaluation) (Result, error)` valida la entrada completa antes de examinar cualquier regla.

- `Layer`: capa compilada existente; una desconocida es `INVALID_INPUT`, no `NO_MATCH`.
- `EffectiveAt`: `time.Time` no cero, años 1–9999 en UTC. Se normaliza a UTC; nunca se sustituye por la hora actual.
- `Context`: mapa no nulo con exactamente todas las variables del schema, incluso las no usadas. Ausentes, nulas y desconocidas se rechazan.

| Tipo del schema | Valores Go aceptados y normalización |
|---|---|
| `bool` / `string` | Tipos Go exactos, sin conversiones implícitas. |
| `int` | `int`, `int8/16/32/64`, `uint`, `uint8/16/32/64` representables en `int64`; `json.Number` con literal JSON entero representable en `int64`; `float64` integral entre −(2^53−1) y 2^53−1. Se normaliza a `int64`. |
| `timestamp` | `time.Time` o string RFC3339 con zona y fracciones opcionales; no cero y años 1–9999 en UTC. Se normaliza a `time.Time` UTC. |

Se rechazan strings numéricos, `float32`, fracciones, NaN, infinito y desbordamientos. `json.Number` no admite exponentes. Para JSON usar `json.Decoder.UseNumber()` y acotar la lectura antes de decodificar; la API Go no puede deshacer asignaciones hechas por el host. No se aceptan mapas o listas como valores del contexto. La normalización crea un mapa nuevo.

## Resolución y trace

1. Valida toda la entrada; si falla, no evalúa reglas ni fallback.
2. Considera solo la capa solicitada y filtra vigencia, scope y condición, en ese orden.
3. Para cada superviviente construye un perfil booleano en el orden de `dimensions`: `true` si la regla restringe esa dimensión. Compara perfiles lexicográficamente desde el último índice hacia el primero, con `true` por encima de `false`; no cuenta simplemente cuántas dimensiones hay.
4. Entre perfiles idénticos, prioriza el valor mayor de `priority`. Si varias reglas empatan en el máximo, devuelve ambigüedad bloqueante. Nunca decide por ID ni orden de carga.

Por ejemplo, con dimensiones `[country, supplier]`, `[false, true]` gana a `[true, false]`, aun si la segunda regla tiene mayor prioridad. Una regla global tiene el perfil `[false, false]`.

`Result` contiene `ruleset_id`, `ruleset_version`, `layer`, `status`, `trace` y, únicamente para `MATCH`, `winner` (`rule_id` y copia profunda del `outcome`). Los candidatos descartados aparecen primero por ID; después los supervivientes por especificidad descendente, prioridad descendente e ID ascendente. No es el orden original del archivo. `specificity` se incluye para supervivientes cuando el perfil no está vacío. `tie_breakers` enumera los supervivientes que comparten el mejor perfil cuando hay más de uno, incluidos los separados por prioridad; no implica por sí solo ambigüedad.

| Status del candidato | Significado / `reason` |
|---|---|
| `NOT_EFFECTIVE` | Fuera de vigencia; sin `reason`. |
| `SCOPE_MISMATCH` | Scope incompatible; `reason` contiene la primera dimensión discrepante en orden alfabético, nunca su valor. |
| `CONDITION_FALSE` | CEL devuelve falso; sin `reason`. |
| `CONDITION_ERROR` | Error ordinario de CEL; solo ID y status, sin texto interno ni `reason`. La regla se descarta y otra puede ganar. |
| `WINNER` | Único ganador. |
| `SHADOWED` | Superviviente superado por el ranking. |
| `AMBIGUOUS` | Integrante del empate en el máximo. |

El trace no copia el contexto. IDs, versiones, nombres de capa y dimensiones sí son datos públicos de diagnóstico; la sanitización de errores CEL no anonimiza esos metadatos.

## Resultados y errores tipados

Inspeccionar errores con `errors.As`, no comparando mensajes. Un error bloqueante nunca devuelve ganador.

| `Result.Status` | Error Go | Interpretación |
|---|---|---|
| `MATCH` | `nil` | Hay ganador. |
| `NO_MATCH` | `nil` | Entrada válida, ninguna regla aplicable. Puede incluir candidatos con error CEL ordinario. |
| `INVALID_INPUT` | `*InvalidEvaluationError` | `Report` explica el fallo previo a CEL. Candidatos vacíos. |
| `AMBIGUOUS_MATCH` | `*AmbiguousMatchError` | `Layer` y `RuleIDs` identifican el empate; conserva trace. Corregir reglas. |
| `LIMIT_EXCEEDED` | `*EvaluationLimitError` | `RuleID` identifica la condición que agotó recursos. Aborta toda la llamada, sin trace parcial ni fallback. |

Un exceso de tamaño de entrada produce **`INVALID_INPUT` con issue `LIMIT_EXCEEDED`**; el status de resultado `LIMIT_EXCEEDED` corresponde al agotamiento durante CEL. [Límites y semántica de cancelación](LIMITS.md): son presupuestos de coste/tamaño, no un timeout duro ni una garantía de latencia universal. Reutilizar condiciones idénticas dentro de la llamada no reduce el coste contabilizado por regla.

## Códigos de validación

`ValidationIssue` tiene `code`, `path`, `message` y `severity`. Automatizar con código y ruta; el mensaje es diagnóstico. Rutas estilo JSON Pointer: `/schema/variables/country`, `/rules/0/condition`, `/context/country`; se escapan `~` y `/`. La ruta raíz es `""`. Issues ordenados por ruta y código; el límite de 1.000 puede truncar la búsqueda con un issue final de límite antes de ordenar. No se promete descubrir todos los errores en una sola compilación: un fallo estructural evita CEL.

| Código | Causa |
|---|---|
| `REQUIRED` | Campo requerido ausente, vacío o nulo según su contrato. |
| `DUPLICATE` | ID de regla o dimensión repetida. |
| `UNKNOWN_TYPE` | Tipo de schema no soportado, dimensión no string o métrica tiered no int. |
| `UNKNOWN_DIMENSION` | Dimensión no declarada en schema o scope fuera de las dimensiones del ruleset. |
| `INVALID_RANGE` | Formato de nombre/ID, intervalo, orden de tiers o fecha efectiva fuera de rango. |
| `INVALID_CEL` | Expresión o entorno CEL inválido, incluidos límites sintácticos del compilador. |
| `CEL_NOT_BOOL` | Tipo de salida estático distinto de bool. |
| `INVALID_OUTCOME` | Kind/payload incompatible, decimal, rate o moneda inválidos. |
| `LIMIT_EXCEEDED` | Entrada fuera de las cotas o reporte truncado. |
| `UNSUPPORTED_COMPOSITION` | Composición distinta de `FIRST_MATCH`. |
| `SCHEMA_MISMATCH` | Versión incompatible; capa, variable o valor de evaluación incompatible con el programa. |

## CLI y verificación

La [guía de CLI](../README.md#cli) incluye comandos ejecutables, flags, archivos JSON, stdout/stderr y códigos de salida. Existe un único binario `rulefare` con `rules validate` y `rules evaluate`; `serve` aún no existe. Cada comando carga y compila los archivos para esa ejecución; un host Go puede reutilizar el `Program` entre llamadas.

Desde la raíz: `go test ./...`, `go test -race ./...` y `go vet ./...`. Los [benchmarks y perfiles](../propuesta/mvp/benchmarks/2026-09-22/t3.9-optimization/README.md) documentan escenarios, entorno y restricciones. El [CI de `main`](https://github.com/Khr0x/rulefare/actions/runs/35778045359) pasó con el corpus técnico de 32 casos; las tarifas reales requieren aceptación comercial independiente.

La [medición de sistema](../propuesta/mvp/benchmarks/2026-09-22/system/README.md) registra CPU en reposo con 10.000 condiciones distintas y syscalls de archivos/red en 4.100 evaluaciones. Incluye controles positivos, trazas y las limitaciones de la muestra. Se reproduce con `bash scripts/measure-system.sh bin/system`.

El [corpus de resolución](testdata/README.md) contiene 32 casos y snapshots explícitos de resultado/trace. `TestGoldenEvaluations` los verifica en orden original e invertido (64 evaluaciones). La [revisión independiente asistida por IA](testdata/FINANCIAL_REVIEW.md) cubre selección, ambigüedad, fallback, vigencias y precisión; las tarifas sintéticas no constituyen reglas comerciales aprobadas.
