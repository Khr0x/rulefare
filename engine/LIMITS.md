# Límites de recursos del motor · v1

`Compile` y `Evaluate` aplican estas cotas también desde la API Go. Son límites fijos, sin configuración por solicitud. La CLI conserva además su límite de **8 MiB de JSON por archivo**.

| Recurso | Límite inclusivo |
|---|---:|
| Reglas por ruleset | 10.000 |
| Variables de schema / entradas de contexto | 256 |
| Dimensiones / entradas de scope por regla | 32 |
| Tiers por outcome | 100 |
| Texto de condición CEL | 4.096 bytes |
| Nodos / profundidad / recursión de parser CEL | 256 / 32 / 32 |
| Nombres de variables, dimensiones y métricas | 64 bytes |
| IDs, versiones, layer y composición | 128 bytes; se conserva la validación de formato específica |
| String de contexto o valor de scope | 4.096 bytes |
| Texto agregado de una evaluación | 65.536 bytes |
| Texto agregado de schema + ruleset | 8.388.608 bytes |
| Literal `json.Number` | 20 bytes y entero representable como `int64` |
| Timestamp textual | 35 bytes, RFC3339Nano, años 1–9999 y no cero |
| Decimal de outcome | 38 dígitos totales, hasta 18 fraccionarios; máximo 39 bytes con punto |
| Patrón regex usado por CEL `matches` | 256 bytes, también si se calcula dentro de CEL |
| Coste CEL por condición | 10.000 unidades |
| Coste CEL acumulado por evaluación | 1.000.000 de unidades |
| Issues de validación devueltos | 1.000 |

Los límites de texto cuentan **bytes UTF-8**, no caracteres. Los tipos del schema siguen siendo `bool`, `int`, `string` y `timestamp`; no se admiten contenedores como valores. Los decimales conservan el formato canónico sin signo, sin ceros iniciales innecesarios ni ceros finales fraccionarios. Un rate sigue estando entre 0 y 1. No se introduce cálculo financiero en el motor.

El presupuesto de evaluación suma layer, nombres del contexto y valores textuales (`string` y `json.Number`). El de compilación suma todos los campos string de schema/ruleset, incluidas condiciones, scopes y outcomes, contando cada aparición. Los valores nativos de tamaño fijo y el overhead de objetos Go no se contabilizan como texto: sus cardinalidades sí están acotadas. Estos presupuestos no son un límite de RSS ni equivalen al tamaño JSON serializado.

## Comportamiento al excederlos

- **Entrada de evaluación sobredimensionada:** `InvalidEvaluationError`, status `INVALID_INPUT`, issue `LIMIT_EXCEEDED`, sin ganador ni candidatos y antes de cualquier CEL. Los paths son `/context` o `/layer`; no se incluye el valor infractor. Un layer mayor de 128 bytes se omite como cadena vacía en el resultado para no reflejarlo completo.
- **Compilación sobredimensionada:** `Program == nil` y un issue `LIMIT_EXCEEDED` con path raíz vacío. El prechequeo termina antes de ordenar claves, construir paths con claves ajenas, convertir decimales o compilar CEL. Una precisión/escala decimal inválida dentro del límite de longitud se informa como `INVALID_OUTCOME`.
- **Coste CEL o patrón regex excesivo:** `EvaluationLimitError{RuleID}`, status `LIMIT_EXCEEDED`, sin ganador ni trace parcial. Aborta toda la resolución incluso si una regla anterior podía ganar. La CLI devuelve código 1, resultado JSON en stdout y mensaje fijo `evaluation resource limit exceeded` en stderr.
- **Error CEL ordinario:** se conserva `CONDITION_ERROR` sin mensaje interno y puede resolverse otra regla, como antes. El consumo de las condiciones ejecutadas, incluso las que devuelven un error ordinario, contribuye al presupuesto acumulado.

Las condiciones idénticas repetidas en una capa pueden reutilizar un resultado sin error dentro de una misma llamada. Cada regla consume igualmente el coste completo registrado para esa condición; el presupuesto acumulado y la regla que lo agota no cambian. No se reutilizan errores ni resultados entre llamadas o contextos.

Los límites inclusivos admiten el valor exacto del umbral; el siguiente byte, dígito o unidad excedente se rechaza. Los presupuestos son por llamada: una evaluación fallida no modifica el programa ni consume recursos de otra evaluación concurrente.

## Alcance del control de coste

Se usa `cel.CostLimit`, con las unidades estimadas por CEL Go v0.30.0, que no equivalen a nanosegundos ni bytes asignados. CEL cobra operaciones después de ejecutarlas. El acumulado se comprueba al terminar cada condición: la condición que cruza el umbral ya se ejecutó y se descarta todo resultado; no se evalúan las siguientes. El coste por condición limita ese trabajo adicional, pero una operación individual puede superar su presupuesto antes del corte.

La medición adversarial encontró que `matches` con un patrón de 4 KiB tardaba unos 40 ms antes de que CEL cancelara. Por eso la implementación de `matches` se envuelve con una comprobación de 256 bytes **antes de entrar a RE2**, tanto para la forma global como la de método, incluyendo `dyn`, constantes y concatenaciones. Los patrones admitidos usan el matcher original de CEL. El exceso cancela la evaluación en vez de devolver un error CEL que pudiera quedar oculto por `|| true`.

No se promete un timeout duro, ni que cualquier entrada admitida cumpla el p99 del fixture de 100 reglas. Los límites acotan trabajo y tamaños; los benchmarks documentan escenarios concretos. Tampoco pueden impedir las asignaciones que un llamador Go realizó antes de invocar el motor. El host debe acotar su propia lectura/decodificación, como ya hace la CLI.

## Elección y evidencia

Las cotas de cardinalidad y sintaxis conservan las de F1. Los nuevos límites dan margen a los fixtures actuales sin permitir valores individuales ilimitados. Las cotas decimales definen el contrato técnico de v1 y deberán contrastarse con el corpus de producto/finanzas. El presupuesto acumulado impide multiplicar libremente el coste máximo por las 10.000 reglas; la carga de benchmark con 10.000 comparaciones simples sigue siendo admisible.

Pruebas en `limits_test.go`, concurrencia, CLI y fuzzing cubren límites inclusivos, exceso agregado, strings multibyte, claves y números de 1 MiB, cardinalidades, precisión decimal, cancelación individual/acumulada, regex calculadas y errores que no pueden quedar ocultos. `BenchmarkResourceLimits` mide entrada máxima y rechazo, excluyendo la construcción de datos adversariales del tiempo medido.

Resultados y comparación de coste de la protección: [medición T3.9](../propuesta/mvp/benchmarks/2026-09-22/t3.9/README.md) y [optimización posterior](../propuesta/mvp/benchmarks/2026-09-22/t3.9-optimization/README.md).
