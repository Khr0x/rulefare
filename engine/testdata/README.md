# Corpus inicial de resolución · pendiente de aprobación

[cases.json](cases.json) contiene diez casos con entrada completa, origen, resultado esperado y trace. Es un borrador técnico para revisión de producto/finanzas; `review_status: pending_product_finance` **no acredita aprobación comercial**. Los valores son sintéticos, sin datos de clientes.

Se reutilizan [schema.json](schema.json), [ruleset.json](ruleset.json) y sus ratios/importes de demostración. [hierarchy_ruleset.json](hierarchy_ruleset.json) transcribe las seis candidatas del [ejemplo de jerarquía §3](../../propuesta/mvp/travel_commission_engine_jerarquia_reglas.md#ejemplo-completo), con ratio sintético uniforme `0.12`: ahí se revisa el orden de selección, no una tarifa comercial.

| Caso | Qué debe revisar producto/finanzas | Esperado |
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

`Dimensions` se serializa **de menor a mayor rango**. El vector de especificidad conserva ese orden; se compara desde el último índice hacia el primero. La guía de API se corrigió para coincidir con el código y el plan; no se cambió el algoritmo ni las dimensiones de un ruleset publicado.

## Ejecutar la revisión técnica

Desde la raíz:

```sh
go test ./engine -run '^TestGoldenEvaluations$' -v
go test -race ./...
```

La prueba lee JSON con `UseNumber` y rechaza campos desconocidos o documentos adicionales. Compara el resultado JSON completo, incluido orden del trace, outcome y errores tipados/reportes cuando corresponda. Cada caso se ejecuta también con el orden de reglas invertido. Las expectativas se escribieron desde los contratos y ejemplos; no hay opción de regenerarlas automáticamente usando el motor como oráculo. `go test ./...` y el workflow existente incluyen esta prueba; todavía no se ha verificado su ejecución remota.

Son casos iniciales de F1. Las pruebas unitarias adicionales cubren prioridad, ambigüedad, errores CEL y agotamiento de recursos, pero no se presentan como corpus comercial aprobado. El corpus tampoco valida cálculo de comisiones, redondeo, paquetes, cancelación, ledger o conciliación: esos comportamientos pertenecen a fases posteriores.

## Registro de revisión

| Revisión | Estado | Revisor / fecha / evidencia |
|---|---|---|
| Técnica local | Casos, suite, race y vet pasan | Evidencia local de desarrollo, 2026-09-22 |
| Producto | Pendiente | Sin aprobación recibida |
| Finanzas | Pendiente | Sin aprobación recibida |
| CI del commit de entrega | Pendiente | Sin ejecución remota verificada |

Para aprobar, revisar las entradas y los resultados completos de `cases.json`, confirmar o corregir las tasas/importes de demostración y añadir casos representativos del negocio real. Registrar responsable, fecha, referencia de aprobación y commit/hash de los archivos revisados. Solo después actualizar el estado de revisión; un test verde no reemplaza esa aprobación. Cualquier cambio de reglas, schema o expectativas exige revisar la versión afectada.
