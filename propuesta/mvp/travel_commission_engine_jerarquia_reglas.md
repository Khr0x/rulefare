# Travel Commission Engine — Jerarquía y resolución de reglas

Documento complementario a:

> `../otras-fases/travel_commission_engine_stack_arquitectura.md`

El documento de negocio define la jerarquía (Global → Mercado → País → ... → Reserva) y enuncia que "el motor debe determinar cuál es la regla más específica aplicable". Pero no define el algoritmo. Este documento lo especifica.

---

## El problema real (doble)

```text
1. No hay algoritmo
   ¿Cómo se compara la especificidad entre dos reglas candidatas?
   ¿Qué pasa si empatan?

2. Hay una ambigüedad semántica escondida
   "Hotelbeds → 14%" REEMPLAZA el base 12%
   "Luxury +1%" SE SUMA al resultado
   Dos verbos distintos (reemplazar / acumular) conviven
   en la misma jerarquía sin haberse separado.
```

La ambigüedad silenciosa en dinero garantiza disputas futuras entre equipos que configuran reglas con interpretaciones distintas.

---

# 1. Principio fundamental

> La especificidad se compara por rango de dimensión, no por cantidad de condiciones.

Y la segunda mitad:

> Toda regla declara su modo de composición. Nunca se infiere.

---

# 2. Dimensiones de scope y jerarquía versionada

Cada regla declara restricciones sobre dimensiones del modelo canónico:

```json
{
  "name": "Hotelbeds Mexico",
  "scope": {
    "supplier": "hotelbeds",
    "country": "MX"
  }
}
```

Perfil canónico de jerarquía travel, heredado del documento de negocio:

```text
RANGO   DIMENSIÓN        EJEMPLO DE VALOR
──────────────────────────────────────────
0       organization     implícita: todas las reglas viven en una org
1       market           LATAM
2       country          MX
3       channel          B2B | B2C | OTA
4       agency           ag_001
5       branch           br_014
6       agent            carlos.m
7       supplier         hotelbeds
8       product          hotel
9       contract         ctr_hb_2026
```

Propiedades:

```text
✓ El host serializa el orden como Dimensions dentro de la versión
  del ruleset; el engine no hardcodea dimensiones travel
✓ Orden inmutable dentro de una versión publicada; añadir o
  reordenar dimensiones crea una versión nueva
✓ Una regla puede restringir cualquier subconjunto
✓ Restricción ausente = comodín para esa dimensión
```

`organization` sigue fuera del contrato del engine: el host ya debe haber aislado el tenant antes de compilar o evaluar. La tabla anterior es el perfil por defecto del producto, no una lista compilada en el paquete Go.

---

# 3. Especificidad lexicográfica

Dadas dos reglas candidatas A y B, ambas con todas sus restricciones satisfechas:

> Gana la que restringe la dimensión de mayor rango donde difieren.

## Por qué no vale contar condiciones

```text
Regla A: { country: "MX" }            1 condición
Regla B: { supplier: "hotelbeds" }    1 condición

Conteo: empate → ambiguo ✗

Lexicográfico:
  la dimensión diferencial de mayor rango es supplier (7) > country (2)
  B precede a A ✓

Intuición de negocio correcta:
  el acuerdo concreto con el proveedor pisa
  la regla genérica por país.
```

## Ejemplo completo

Reserva: Hotelbeds · México · hotel · contrato 2026

```text
Candidatas que matchean:

R1 { }                                   base global
R2 { market: LATAM }
R3 { country: MX }
R4 { supplier: hotelbeds }
R5 { supplier: hotelbeds, country: MX }
R6 { contract: ctr_hb_2026 }

Ranking lexicográfico (mayor rango primero):

R6 (9) > R5 (7,2) > R4 (7) > R3 (2) > R2 (1) > R1 (-)
```

Comparación entre R5 y R4: la dimensión de mayor rango donde difieren es `country` (2); R5 la restringe → R5 precede. Comparación entre R5 y R3: difieren primero en `supplier` (7) → R5 precede.

---

# 4. Modos de composición

Aquí vive la resolución de la ambigüedad semántica. Cada regla declara:

```json
{
  "composition": "FIRST_MATCH",
  ...
}
```

## FIRST_MATCH (default)

La regla más específica gana y detiene la cascada:

```text
Base global:   12%
MX override:   15%   (FIRST_MATCH)

Reserva Hotelbeds MX:
ganadora: MX override → 15%   (el 12% queda fuera)
```

Es la semántica de reemplazo: precios base, comisiones estándar.

## STACK

Se aplican en orden de menos a más específica; cada una transforma el resultado:

```text
Base global:    12%
Luxury bonus:   +1%   (STACK)

Reserva hotel luxury:
12% → +1% → 13%
```

Es la semántica de ajuste: bonuses, overrides incrementales, fees.

## Regla de oro

```text
✗ Mezclar ambos modos sin declararlos
✗ Inferir composición del tipo de outcome
✗ STACK ilimitado: límite configurable (ej. máx 5 niveles)
  y orden determinístico documentado
```

Ejemplo combinado final:

```text
Reserva: Hotelbeds MX luxury

Cascada STACK declarada:
  BASE_GLOBAL      12%
  LUXURY_BONUS     +1%    → 13%
Ganador FIRST_MATCH sobre el tronco:
  HOTELBEDS_MX     reemplaza base por 15%

Resultado: 15%
Trace registra las tres reglas y sus papeles.
```

El diseño exacto de qué entra en cada tronco es configurable por organización; lo innegociable es que quede explícito en configuración y reflejado íntegro en el trace.

---

# 5. Algoritmo de evaluación

```text
ENTRADA: booking + contexto + agregados + reglas ACTIVE vigentes

1. CANDIDATAS
   Prefiltro SQL por scope indexado (§8)
   → conjunto pequeño de reglas potencialmente aplicables

2. MATCH EXACTO
   CEL evalúa conditions + scope sobre el contexto completo
   → solo quedan reglas plenamente aplicables

3. RANKING
   Ordenación lexicográfica por jerarquía fija

4. RESOLUCIÓN
   FIRST_MATCH: primera del ranking gana
   STACK:       aplicación ordenada ascendente

5. EMPATES (mismo perfil de especificidad)
   priority explícita mayor gana
   si persiste empate exacto:
     runtime  → error bloqueante (fail closed)
     validación → warning en el reporte pre-deploy

6. TRACE
   Registrar candidatos, ganadores, descartes y razones
```

Propiedad esencial: pasos 1-5 son una función pura sobre entrada + versión de reglas. Reproducible por construcción.

---

# 6. EvaluationTrace: explainability estructural

Todo CalculationResult incorpora:

```json
{
  "resolution": {
    "mode": "FIRST_MATCH",
    "candidates": [
      { "rule": "BASE_GLOBAL",      "outcome": "skipped", "reason": "precedida por HOTELBEDS_MX" },
      { "rule": "HOTELBEDS",        "outcome": "skipped", "reason": "precedida por HOTELBEDS_MX" },
      { "rule": "MEXICO_OVERRIDE",  "outcome": "skipped", "reason": "precedida por HOTELBEDS_MX" },
      { "rule": "HOTELBEDS_MX",     "outcome": "winner" },
      { "rule": "LUXURY_BONUS",     "outcome": "stacked", "applied_after": "HOTELBEDS_MX" }
    ],
    "tie_breakers_used": []
  }
}
```

Conecta directo con el requisito de negocio "¿Por qué?":

```text
Comisión: €425
← HOTELBEDS_MX ganó porque restringe supplier+country
← MEXICO_OVERRIDE quedó fuera por precedencia lexicográfica
← trace persistido junto al snapshot fiscal y FX
```

Reclamaciones, auditoría y soporte se responden con datos, no con memoria de empleados.

---

# 7. Validación previa al deploy

Enganchada al workflow de aprobaciones (maker-checker). Una regla nueva no llega a ACTIVE sin pasar:

```text
1. LINT
   Esquema válido
   CEL compila y tipa contra el esquema canónico del contexto

2. SIMULACIÓN
   Corpus dorado de reservas representativas
   + últimas N reservas reales (referencias opacas, sin PII)
   Ejecutar ruleset actual vs ruleset propuesto
   → diff de resultados:
     "esta regla altera el 3,2% de reservas,
      impacto medio +€4,10 por reserva"

3. CONFLICT SCAN
   Detección simbólica de solapamientos con reglas activas:
   ¿existe booking teórico donde ambas apliquen y empaten?

4. APROBACIÓN
   Maker-checker del flujo de autorización

5. ACTIVACIÓN
   Vigencia futura (valid_from), sin despliegues manuales
```

El paso 2 convierte el cambio de reglas en una decisión informada: el aprobador ve el impacto antes de firmar.

Política configurable:

```yaml
rules_validation:

  simulation:
    corpus_min_bookings: 1000
    fail_on_impact_above: 25%   # % de reservas alteradas

  conflicts:
    exact_tie: block            # warn | block

  lint:
    cel_strict_typing: true
```

---

# 8. Performance: prefiltro y CEL final

Miles de reglas activas por organización son viables con disciplina simple:

```sql
rule_scope
---------------------------
rule_id
dimension    TEXT
value        TEXT

CREATE INDEX ON rule_scope (dimension, value);
```

```text
1. Lookup indexado de candidatas por dimensiones duras
   (supplier, country, product...) → decenas de reglas

2. CEL decide el match fino (fechas, importes, agregados)

3. Ranking sobre el conjunto resultante: trivial
```

CEL nunca recorre el catálogo completo. El catálogo completo jamás sale de PostgreSQL sin índice.

---

# 9. Qué NO construir

```text
✗ Prioridades numéricas globales como mecanismo primario
  (la jerarquía ya ES la prioridad; los números ocultan conflictos)
✗ Herencia dinámica entre reglas (reglas que extienden reglas)
✗ Motor RETE o similar: el dominio es selección + outcome,
  no inferencia encadenada
✗ Resolución de conflictos automática "inteligente":
  ante duda, fallar y avisar supera a adivinar
✗ Recalcular histórico con ranking nuevo sin versión explícita
```

---

# 10. Fases

Qué entra en cada versión de este módulo está clasificado en:

> `travel_commission_engine_alcance_v1.md` (sección 7)

Resumen: ranking lexicográfico + FIRST_MATCH + trace en v1;
STACK y conflict scan automático en v2;
simulador what-if sobre histórico en v3.

---

# 11. Impacto en el resto de la arquitectura

```text
Negocio §3   jerarquía conceptual → algoritmo formal
§6   Rule Engine      scope + composition amplían el DSL
Agregación            period rules usan el mismo ranking;
                      sus triggers también pueden solapar
Explainability        EvaluationTrace es su materia prima
Autorización          validación pre-deploy = gate del approval;
                      simulación visible al checker
Eventos               rule.activated dispara invalidación de caches
§23   Versionado        el ranking es parte de la versión efectiva:
                      cambios de jerarquía versionan también
Webhooks              resolution incluida en commission.calculated
```

---

# 12. Resumen

```text
              REGLAS ACTIVAS VIGENTES
                        │
                        ▼
             ┌─────────────────────┐
             │ PREFILTRO INDEXADO  │  miles → decenas
             └──────────┬──────────┘
                        ▼
             ┌─────────────────────┐
             │ MATCH CEL EXACTO    │  decenas → pocas
             └──────────┬──────────┘
                        ▼
             ┌─────────────────────┐
             │ RANKING             │  lexicográfico
             │ JERARQUÍA FIJA      │  por rango de dimensión
             └──────────┬──────────┘
                        ▼
             ┌─────────────────────┐
             │ COMPOSICIÓN         │  FIRST_MATCH | STACK
             │ DECLARADA           │  nunca inferida
             └──────────┬──────────┘
                        ▼
                CALCULATION RESULT
             + evaluation_trace completo
                        │
        ┌───────────────┴────────────────┐
        ▼                                ▼
   EXPLAINABILITY                  AUDITORÍA
   "¿por qué este importe?"    "¿quién aprobó esta regla?"
```

Principios finales:

> El rango de la dimensión decide, no el número de condiciones.

> Reemplazar y acumular son verbos distintos: cada regla declara el suyo.

> Un empate no resuelto es un error, no una elección silenciosa.

> Ninguna regla se activa sin mostrar antes a cuántas reservas afecta.
