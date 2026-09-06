# Travel Commission Engine — Reglas basadas en agregación

Documento complementario a:

> `travel_commission_engine_stack_arquitectura.md`

La propuesta técnica actual define el Rule Engine sobre CEL. CEL es adecuado para condiciones sobre la reserva actual, pero es **stateless**: no puede expresar condiciones sobre datos históricos o acumulados. Este documento resuelve ese hueco.

---

## El problema

Muchos tipos de regla del producto requieren contexto histórico:

```text
Bonus mensual        "si volumen del mes > €50.000 → +2%"
Escalonada           "10% hasta 100k, 12% a partir de 100k"
Rappel retroactivo   "+1% retroactivo si producción anual > €1M"
Bonus por hito       "+€500 al llegar a 50 reservas"
```

Ninguna de estas condiciones es evaluable con CEL sobre una única reserva:

```text
supplier == 'hotelbeds' && country == 'MX'          ✓ CEL

sum(volumen_mes_actual) > 50000                     ✗ CEL
```

---

# 1. Principio fundamental

> CEL nunca consulta datos. CEL recibe datos ya calculados.

La plataforma mantiene los acumulados y los inyecta en el contexto de evaluación como valores planos. CEL sigue siendo puro, portable y auditable.

Y la segunda mitad del principio:

> La condición decide si una regla aplica. El outcome, tipado, decide qué se calcula.

Los resultados complejos (tiers, splits, rappels) no viven en expresiones: viven en estructuras declarativas interpretadas por código determinista con Decimal.

---

# 2. Arquitectura de tres piezas

```text
                 BOOKING EVENT
                      │
                      ▼
         ┌─────────────────────────┐
         │ AGGREGATION ENGINE      │
         │                         │
         │ contribuciones          │
         │ append-only             │
         │ períodos y dimensiones  │
         └───────────┬─────────────┘
                     │ valores inyectados
                     ▼
         ┌─────────────────────────┐
         │ CONDITION LAYER (CEL)   │
         │                         │
         │ booking + context       │
         │ + aggregate.*           │
         └───────────┬─────────────┘
                     │ reglas que aplican
                     ▼
         ┌─────────────────────────┐
         │ OUTCOME LAYER (tipado)  │
         │                         │
         │ percentage | fixed      │
         │ tiered | split          │
         │ bonus | rappel          │
         └───────────┬─────────────┘
                     │
                     ▼
               CALCULATION RESULT
```

---

# 3. Agregados declarativos

Un agregado se define en configuración, nunca en código:

```yaml
aggregates:

  - id: supplier_volume_monthly
    description: Volumen neto por proveedor y mes
    measure: sum
    field: booking.amounts.net
    filter: "product.type == 'hotel'"
    dimensions:
      - organization_id
      - supplier_id
    window: CALENDAR_MONTH
    currency: organization_base
```

Componentes:

```text
measure      sum | count | avg
field        campo monetario o contador del modelo canónico
filter       expresión CEL sobre la reserva (mismo lenguaje)
dimensions   ejes de agrupación
window       CALENDAR_MONTH | QUARTER | YEAR | ROLLING_30D | LIFETIME
currency     divisa de normalización (ver multi-divisa)
```

Definir un agregado nuevo debe ser configuración versionada, igual que una regla.

---

# 4. Inyección en el contexto de evaluación

En tiempo de cálculo, la plataforma resuelve los agregados referenciados por las reglas candidatas y los añade al contexto:

```text
context = {
    booking:      {...},
    agent:        {...},
    contract:     {...},

    aggregate: {
        supplier_volume_monthly: "412350.00",
        bookings_count_ytd:      8421
    }
}
```

Desde CEL, un agregado es un dato más:

```text
aggregate.supplier_volume_monthly > 50000
```

Ventajas frente a alternativas:

```text
✓ CEL permanece estándar y portable
✓ Los valores quedan registrables en el snapshot
✓ Sin funciones custom que rompan tooling y auditoría
✓ Fácil de simular y de testear
```

---

# 5. Outcome layer: tipos de resultado con estado

El outcome deja de ser solo `{ type: percentage }` y pasa a un conjunto cerrado de tipos interpretados por código:

## Percentage / Fixed (ya existentes)

```json
{ "type": "percentage", "value": "0.17" }
{ "type": "fixed",      "value": "25.00" }
```

## Tiered (escalonada)

```json
{
  "type": "tiered",
  "measure": "aggregate.supplier_volume_ytd",
  "tiers": [
    { "up_to": "1000000", "rate": "0.10" },
    { "up_to": "3000000", "rate": "0.12" },
    { "up_to": null,      "rate": "0.14" }
  ],
  "mode": "marginal"
}
```

Dos semánticas posibles:

```text
FLAT      el tramo alcanzado aplica a todo el importe
MARGINAL  cada tramo aplica solo a la porción que cubre
```

Ejemplo con reserva de 100k y volumen previo de 950k:

```text
Tramos: 10% hasta 1M · 12% desde 1M

FLAT      12% × 100.000  = 12.000
MARGINAL  10% × 50.000 + 12% × 50.000 = 11.000
```

El modo es explícito en la regla. Nunca implícito.

## Split (distribución)

```json
{
  "type": "split",
  "entries": [
    { "beneficiary": "agency_matrix", "share": "0.50" },
    { "beneficiary": "agent",         "share": "0.30" },
    { "beneficiary": "franchise",     "share": "0.12" },
    { "beneficiary": "affiliate",     "share": "0.08" }
  ]
}
```

Validación obligatoria al guardar la regla:

```text
suma de shares == 1 exactamente (Decimal)
```

## Bonus / Rappel (ajustes de periodo)

Se ejecutan en cierre de periodo, no por reserva. Ver sección siguiente.

Regla de oro del outcome layer:

> Conjunto cerrado de tipos. Extenderlo es una decisión de producto con tests golden, no un escape a código arbitrario.

---

# 6. Dos tiempos de ejecución

Esta es la distinción clave que resuelve los rappels retroactivos:

```text
BOOKING RULE
────────────────────────────────────────
Se evalúa por reserva, en tiempo de ingesta.
Condiciones: booking + context + aggregate.* (valor vivo)
Outcomes: percentage | fixed | tiered | split

PERIOD RULE
────────────────────────────────────────
Se evalúa una vez por periodo cerrado, en batch.
Condiciones: aggregate.* FINALIZADO
Outcomes: bonus | rappel | clawback
Generan entradas ADJUSTMENT en el ledger
```

Un mismo DSL, dos momentos de disparo. El rappel retroactivo deja de ser un caso especial y pasa a ser una period rule normal.

Ejemplo completo de period rule:

```json
{
  "name": "Hotelbeds rappel anual",
  "kind": "period_rule",
  "window": "YEAR",
  "when": {
    "expression": "aggregate.supplier_volume_year >= 1000000"
  },
  "then": {
    "type": "rappel",
    "base": "period_commission_total",
    "value": "0.01"
  }
}
```

Al cierre:

```text
Volumen anual final:            3.240.000 EUR   ✓
Comisiones acumuladas del año:    486.000 EUR
Rappel 1%:                        + 4.860 EUR

Ledger:
RAPPEL_ADJUSTMENT  +4.860   (append-only, diciembre)
```

Las comisiones originales jamás se modifican.

---

# 7. Ciclo de vida del periodo

```text
        ┌──────────┐     fin de mes      ┌──────────┐    period rules ok   ┌───────────┐
        │   OPEN   │ ──────────────────▶ │  CLOSED  │ ───────────────────▶ │ FINALIZED │
        └──────────┘                     └──────────┘                      └───────────┘
        recibe reservas                  sin reservas nuevas               inmutable
        agregado vivo                    ajustes en curso                  reporting oficial
```

## Llegadas tardías

Reservas que aparecen cuando el periodo ya está CLOSED o FINALIZED. Política configurable por organización:

```yaml
aggregation:

  late_arrival_policy: NEXT_PERIOD
  # NEXT_PERIOD  la reserva cuenta en el periodo abierto
  # REOPEN       reabre versión nueva del periodo,
  #              recalcula y genera ajustes delta
```

`REOPEN` crea siempre una **versión nueva** del periodo; el resultado anterior permanece auditable.

---

# 8. Almacenamiento: contribuciones y materializado

## Contribuciones (append-only, fuente de verdad)

```sql
aggregate_contribution
---------------------------
id
aggregate_id
period_key            -- 2026-08 | 2026 | ...
dimension_values      JSONB
amount                NUMERIC(18,4)
currency              CHAR(3)
fx_snapshot_id        -- si hubo conversión
source_calculation_id -- trazabilidad total
created_at
```

## Valor materializado (reconstruible)

```sql
aggregate_period_value
---------------------------
aggregate_id
period_key
dimension_values JSONB
value            NUMERIC(18,4)
currency         CHAR(3)
status           OPEN | CLOSED | FINALIZED
version
updated_at
```

Invariantes:

```text
✓ Toda contribución referencia la calculation que la originó
✓ El materializado puede borrarse y reconstruirse
  reproduciendo contribuciones en orden
✓ La actualización del materializado ocurre en la misma
  transacción que la calculation y su entrada de ledger
✓ Orden por agregado garantizado (partición por
  aggregate_id + dimension hash)
```

Esto convierte cualquier bug o cambio en un problema de replay, no de reparación manual.

---

# 9. Determinismo con agregados vivos

Un agregado vivo cambia constantemente; un snapshot lo congela:

```json
{
  "aggregates_used": [
    {
      "id": "supplier_volume_monthly",
      "period_key": "2026-08",
      "value": "412350.00",
      "currency": "EUR",
      "status": "OPEN",
      "as_of": "2026-08-20T14:03:11Z",
      "definition_version": 3
    }
  ]
}
```

Determinismo extendido:

```text
mismo booking
+
misma versión de reglas
+
mismo snapshot de FX
+
mismo snapshot fiscal
+
mismo snapshot de agregados usados
=
mismo resultado completo
```

Para period rules el problema desaparece: operan sobre FINALIZED, que es inmutable por definición.

---

# 10. Multi-divisa en agregados

Sumar importes solo tiene sentido en una divisa. Cada contribución monetaria se normaliza a la divisa del agregado en el momento de registrarla, usando el FX snapshot del cálculo:

```text
Booking 2.500 USD
FX snapshot: USD/EUR 0.9234

Contribución: 2308.50 EUR
(fx_snapshot_id referencia el rate usado)
```

Nunca se suma en divisas mezcladas ni se re-convierte el total. Coherente con la sección de multi-divisa: cada conversión deja constancia exactamente una vez.

---

# 11. Ejemplos end-to-end

## Bonus mensual (booking rule)

```text
Regla:
  when: supplier == 'hotelbeds' &&
        country == 'MX' &&
        aggregate.supplier_volume_monthly > 50000
  then: bonus percentage 0.02 sobre comisión

Flujo:
  1. Entra reserva de 30.000 USD
  2. Contribución al agregado (TX única con el cálculo):
     volumen mes pasa a 51.200 EUR
  3. CEL evalúa con 51.200 inyectado → true
  4. Resultado registra snapshot del agregado usado
```

## Escalonada progresiva (booking rule)

```text
Regla: tiered marginal sobre supplier_volume_ytd
Reserva: 80.000 EUR · volumen previo YTD: 960.000

Tramos: 10% hasta 1M · 12% desde 1M

40.000 × 10%  = 4.000
40.000 × 12%  = 4.800
Comisión      = 8.800 EUR
```

## Rappel retroactivo (period rule)

Ver ejemplo de la sección 6.

---

# 12. Qué NO construir

```text
✗ Funciones custom en CEL que consulten datos
✗ SQL embebido en reglas
✗ Código arbitrario de usuarios en el outcome layer
✗ Agregados en memoria sin persistencia
✗ Ventanas rodantes de alta resolución en la v1
  (ROLLING_30D diario programado basta)
```

---

# 13. Fases

Qué entra en cada versión de este módulo está clasificado en:

> `../mvp/travel_commission_engine_alcance_v1.md` (sección 7)

Resumen: en v1 los agregados se calculan on-demand con SQL
(los volúmenes lo permiten) y solo outcomes percentage/fixed;
acumuladores persistentes y period rules en v2;
ventanas rodantes y simulador sobre agregados históricos en v3.

---

# 14. Impacto en el resto de la arquitectura

```text
§5bis  FX                contribuciones normalizadas con snapshot
§6     Rule Engine       DSL ampliado: kind + outcome types
§7     Calculation       capa de outcome interpretada, función pura
§16    Webhooks          nuevos eventos: period.closed, adjustment.created
§22    Ledger            ADJUSTMENT entries; jamás mutar originales
§23    Versionado        definiciones de agregados también versionan
§24    Workflows         cierre de periodo = worker batch programado;
                         candidato natural a Temporal más adelante
Alcance v1             fuera del núcleo; agregación on-demand por SQL,
                         persistente en v2 (ver alcance_v1)
```

---

# 15. Resumen

```text
ANTES (hueco detectado)

  CEL ──── ¿volumen del mes? ──── ✗ imposible



DESPUÉS (propuesta)

  Aggregation Engine ──▶ aggregate.* ──▶ CEL (condición)
                                              │
                                              ▼
                                      Outcome tipado
                                       percentage | fixed
                                       tiered | split
                                              │
                              ┌───────────────┴───────────────┐
                              ▼                               ▼
                       BOOKING RULES                   PERIOD RULES
                       por reserva                     por cierre
                                                       bonus · rappel
                                                       clawback
                              └───────────────┬───────────────┘
                                              ▼
                                    LEDGER append-only
                                     + snapshots totales
```

Principios finales:

> CEL recibe datos; nunca los busca.

> La condición es CEL; el resultado es tipado.

> Lo retroactivo es un ajuste nuevo, nunca una mutación.

> Todo valor usado en una decisión queda congelado en el snapshot.
