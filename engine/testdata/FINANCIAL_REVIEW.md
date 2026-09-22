# Revisión de dominio financiero de F1

Fecha: 2026-09-22. Base: `df48c04718c89cd5d68f7bb3f02f5b74048ff175`.
Revisión independiente: agente `financial_corpus_review`, solicitado por el responsable del proyecto. Alcance: contrato de selección, fixtures sintéticos y 32 expectativas de [cases.json](cases.json). Es una revisión técnica asistida por IA, no una certificación ni una aprobación humana de tarifas.

## Resultado

No se identificó un defecto reproducible de selección frente al contrato actual. Se amplió el corpus de 10 a 32 casos para conservar explícitamente las decisiones de mayor impacto financiero. El agente contrastó los resultados esperados completos, incluidos outcome, trace, especificidad, desempates y errores tipados. Las expectativas se redactaron desde el contrato, sin generarlas con `Evaluate`.

No fue necesario cambiar el motor. Se extendió el harness para verificar también `AmbiguousMatchError` y `EvaluationLimitError`, incluidos los IDs responsables. Los 32 casos pasan en orden original e invertido (64 evaluaciones). La suite completa, detector de carreras y `go vet` pasan localmente. La [CI del commit de merge `09ff1e2`](https://github.com/Khr0x/rulefare/actions/runs/35778045359) pasó con los 32 casos.

Fuentes: [API implementada](../README.md), [plan F1](../../propuesta/mvp/f1_motor_reglas_plan_implementacion.md), [jerarquía](../../propuesta/mvp/travel_commission_engine_jerarquia_reglas.md), `evaluate.go`, `ranking.go`, `validate.go` y los fixtures de este directorio. Las capacidades de fases posteriores de la propuesta no se atribuyen a F1.

## Decisiones verificadas

| Riesgo | Contrato y protección del corpus |
|---|---|
| Elegir una comisión por su importe | Primero especificidad, después prioridad; el importe no participa. Se prueban prioridad extrema y tasa menor ganadora. |
| Resolver silenciosamente un empate | Un empate máximo devuelve `AMBIGUOUS_MATCH`, incluso con outcomes idénticos; el origen de la decisión sigue siendo ambiguo. |
| Activar fallback con entrada inválida | Schema inválido o capa inexistente bloquean antes de CEL. El agotamiento de recursos también bloquea, sin ganador ni trace parcial. |
| Ocultar un error de condición detrás de un fallback | Un error CEL ordinario descarta esa regla con `CONDITION_ERROR` sanitizado; otra regla puede ganar. Es el contrato actual, ahora explícito en el corpus. |
| Interpretar inactividad como cancelación | `active` es una variable ordinaria: solo afecta a reglas que la consultan. No representa un veto global ni revierte comisiones. |
| Confundir ausencia de regla con comisión cero | `NO_MATCH` no tiene ganador ni outcome; una tasa cero explícita devuelve `MATCH`. El host debe tratarlos de forma distinta. |
| Desplazar vigencias o umbrales | Intervalos `[from,to)`, zonas horarias equivalentes y umbral estricto quedan cubiertos. `travel_date` es independiente de `EffectiveAt`. |
| Calcular tiers prematuramente | F1 devuelve el plan completo también en sus límites; no selecciona tramo ni decide cálculo marginal o retroactivo. |
| Perder precisión | Enteros JSON con `UseNumber` alrededor de 2^53 y payload decimal de 18 posiciones se preservan exactamente. |

## Decisiones necesarias para reglas comerciales

El schema valida estructura, tipos y recursos; no define todas las políticas de negocio. El host o las reglas deben establecer valores permitidos, importes negativos, cadenas vacías, códigos válidos y relación entre proveedor y contrato. `amount_minor` no lleva por sí mismo moneda ni escala. Los scopes comparan strings exactamente, incluidas mayúsculas.

Antes de adoptar tarifas reales, el responsable debe definir su fuente contractual, moneda y escala, base de cálculo, significado de `active`, fechas aplicables y aceptación del fallback tras un error CEL ordinario. Si un fallo de condición debe detener el proceso comercial, el host debe revisar `CONDITION_ERROR` y aplicar esa política; no debe inferir ausencia de errores a partir de `MATCH`.

El importe sintético `9007199254740993.000000000000000001 MXN` comprueba conservación textual; no demuestra que sea liquidable en MXN. Redondeo monetario, impuestos, conversión, aplicación de tiers, cancelaciones, ledger y conciliación pertenecen a fases posteriores. Las tasas e importes del corpus no proceden de contratos reales.

La revisión solicitada cubre el corpus técnico de F1 sin requerir un equipo de producto/finanzas inexistente. La aceptación comercial corresponde al responsable del proyecto cuando incorpore datos reales. Este informe no cierra por sí solo F1: deben completarse los demás criterios y conservarse la evidencia de CI del commit de entrega.
