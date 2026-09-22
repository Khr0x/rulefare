# Corpus de resolución · revisión técnica asistida por IA

[cases.json](cases.json) contiene **32 casos** con entrada completa, origen, resultado esperado y trace. Un agente independiente con enfoque financiero revisó sus expectativas contra el contrato de F1, por solicitud del responsable del proyecto ante la ausencia de equipo de producto/finanzas. `review_status: ai_technical_review_completed` registra esa revisión técnica; no acredita aprobación humana de tarifas comerciales. Los valores son sintéticos, sin datos de clientes. Véase el [informe de revisión](FINANCIAL_REVIEW.md).

Se reutilizan [schema.json](schema.json), [ruleset.json](ruleset.json) y sus ratios/importes de demostración. [hierarchy_ruleset.json](hierarchy_ruleset.json) transcribe las seis candidatas del [ejemplo de jerarquía §3](../../propuesta/mvp/travel_commission_engine_jerarquia_reglas.md#ejemplo-completo), con ratio sintético uniforme `0.12`: ahí se revisa el orden de selección, no una tarifa comercial.

| Caso | Comportamiento revisado | Esperado |
|---|---|---|
| `supplier_country` | Proveedor + país preceden a proveedor, país y global | `HOTELBEDS_MX`, percentage `0.17` |
| `global` | Sin scopes coincidentes queda el global | `BASE_GLOBAL`, percentage `0.12` |
| `country` | País precede a global sin proveedor coincidente | `MEXICO`, percentage `0.15` |
| `fixed` | El payload fijo se conserva sin cálculo | `HOTELBEDS_FIXED`, fixed `25 MXN` |
| `valid_from_inclusive` | La regla aplica exactamente en el inicio | `HOTELBEDS_MX`, percentage `0.17` |
| `valid_to_exclusive` | Al llegar al final deja de aplicar; proveedor precede a país | `HOTELBEDS_FIXED`, fixed `25 MXN` |
| `tiered` | Se devuelve el plan de tiers completo, sin elegir tramo | `CONTRACT_TIERS`, tasas `0.7/0.8/0.9` |
| `no_match` | Una capa existente puede no tener regla aplicable | `NO_MATCH`, sin ganador |
| `invalid_before_fallback` | Falta `country`; ninguna regla debe ejecutarse | `INVALID_INPUT`, `REQUIRED` en `/context/country`, trace vacío |
| `hierarchy_six_candidates` | Orden canónico `R6 > R5 > R4 > R3 > R2 > R1` | Gana contrato `R6` |

[financial_safety_ruleset.json](financial_safety_ruleset.json) añade escenarios aislados de prioridad, ambigüedad, errores y precisión. Los 22 casos añadidos cubren:

| Casos | Comportamiento revisado |
|---|---|
| `threshold_exact`, `threshold_above` | Umbral estricto: 100000 queda fuera; 100001 entra |
| `inactive_is_explicit` | `active=false` solo afecta a condiciones que lo consultan |
| `before_valid_from`, `timezone_same_instant` | Instante anterior y equivalencia de zonas horarias |
| `travel_date_before_condition` | La fecha de viaje es una condición distinta de la vigencia |
| `tier_metric_99999`, `tier_metric_100000`, `tier_metric_299999`, `tier_metric_300000` | El plan completo se conserva en los límites; F1 no calcula tramos |
| `scope_case_sensitive`, `unknown_layer` | Comparación exacta de scope y rechazo de capa inexistente |
| `priority_not_largest_rate`, `specificity_before_priority_extremes` | Gana la precedencia contractual, no la mayor comisión; especificidad antes de prioridad |
| `tie_ambiguous`, `tie_equal` | El empate máximo bloquea incluso con outcomes idénticos |
| `ordinary_cel_error_allows_fallback`, `resource_limit_blocks_fallback` | Error CEL ordinario descarta la regla; agotamiento bloquea toda la evaluación |
| `zero_is_a_match`, `no_match_is_not_zero` | Comisión cero explícita y ausencia de regla son resultados distintos |
| `exact_integer_and_decimal`, `adjacent_large_integer` | Enteros exactos alrededor de 2^53 y conservación textual de decimales |

`Dimensions` se serializa **de menor a mayor rango**. El vector de especificidad conserva ese orden; se compara desde el último índice hacia el primero. La guía de API se corrigió para coincidir con el código y el plan; no se cambió el algoritmo ni las dimensiones de un ruleset publicado.

## Ejecutar la revisión técnica

Desde la raíz:

```sh
go test ./engine -run '^TestGoldenEvaluations$' -v
go test -race ./...
```

La prueba lee JSON con `UseNumber` y rechaza campos desconocidos o documentos adicionales. Compara el resultado JSON completo, incluido orden del trace, outcome y errores tipados/reportes cuando corresponda. Cada caso se ejecuta también con el orden de reglas invertido: **64 evaluaciones verificadas**. Las expectativas se escribieron desde los contratos y ejemplos; no hay opción de regenerarlas automáticamente usando el motor como oráculo. `go test ./...` y el workflow incluyen esta prueba; el [CI del merge en `main`](https://github.com/Khr0x/rulefare/actions/runs/35778045359) pasó con la ampliación.

Son casos técnicos de F1 revisados de forma independiente. Las pruebas unitarias adicionales complementan su cobertura; los datos sintéticos no se presentan como tarifas comerciales aprobadas. El corpus tampoco valida cálculo de comisiones, redondeo, paquetes, cancelación, ledger o conciliación: esos comportamientos pertenecen a fases posteriores.

## Registro de revisión

| Revisión | Estado | Revisor / fecha / evidencia |
|---|---|---|
| Técnica local | Casos, suite, race y vet pasan | Evidencia local de desarrollo, 2026-09-22 |
| Dominio financiero asistido por IA | Completada para los 32 casos | Agente independiente `financial_corpus_review`, 2026-09-22; [informe](FINANCIAL_REVIEW.md) |
| Tarifas y políticas reales | Fuera de la aceptación técnica de estos fixtures | Requieren datos y decisión del responsable antes de uso comercial |
| CI de la base `df48c04` | Verde | [Run de main](https://github.com/Khr0x/rulefare/actions/runs/35773582167), corpus anterior de 10 casos |
| CI de la ampliación a 32 casos | Verde en `main` | [Run `35778045359`](https://github.com/Khr0x/rulefare/actions/runs/35778045359), commit `09ff1e2` |

Para adoptar reglas comerciales, el responsable del proyecto debe aportar contratos o ejemplos reales, confirmar tasas, moneda, escala, condiciones y fallback, y registrar su decisión con fecha y commit/hash del corpus revisado. No hace falta crear un equipo de producto: la aceptación puede hacerla el responsable con conocimiento del negocio. Cualquier cambio de reglas, schema o expectativas exige revisar la versión afectada.
