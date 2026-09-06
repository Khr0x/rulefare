# Travel Commission Engine — Liquidación y Settlements

Documento complementario a:

> `travel_commission_engine_stack_arquitectura.md`

El documento de negocio promete el ciclo completo de la comisión:

> `ESTIMATED → CONFIRMED → ACCRUED → PAYABLE → PAID`

Hasta ahora la documentación técnica diseña el asiento contable pero no el proceso que lo rodea. Este documento cierra ese hueco: sin él, la plataforma calcula deudas perfectas que nadie ejecuta.

---

# 1. Principio fundamental

> El estado operativo avanza por eventos verificables, jamás por tiempo transcurrido solo.

Cada transición del ciclo de vida tiene un disparador concreto: un evento de conector, un resultado de reconciliación, una acción humana aprobada. La máquina de estados es la misma para SaaS y self-hosted.

Y en línea con los documentos previos:

> Cobrar y pagar son mutaciones financieras: pasan por outbox, idempotencia y maker-checker como todo lo demás.

---

# 2. Ciclo de vida de la comisión

## Máquina de estados

```text
                checkout / fin de viaje
   ┌───────────┐ ─────────────────────▶ ┌───────────┐
   │ ESTIMATED │                        │ CONFIRMED │
   └─────┬─────┘                        └─────┬─────┘
         │ cancelación                        │ factura conciliada
         ▼                                    │ o fecha de devengo
   ┌───────────┐                              ▼
   │ CANCELLED │                        ┌───────────┐
   └───────────┘                        │  ACCRUED  │
                                        └─────┬─────┘
                                              │ recon OK + sin disputa
                                              ▼
   ┌───────────┐  disputa abierta     ┌───────────┐   pago ejecutado
   │ DISPUTED  │ ◀────────────────────│  PAYABLE  │ ──────────────▶ ┌──────┐
   └─────┬─────┘                      └───────────┘                 │ PAID │
         │ resolución                                               └──────┘
         ▼                                                              │
   vuelve al estado origen                                              ▼
                                                                  ┌──────────┐
                                                                  │ CLAWBACK │
                                                                  └──────────┘
```

## Transiciones y disparadores

| Desde | Hacia | Disparador | Fuente |
|---|---|---|---|
| ESTIMATED | CONFIRMED | checkout / fin de viaje | evento conector o fecha política |
| ESTIMATED | CANCELLED | cancelación pre-consumo | evento conector |
| CONFIRMED | ACCRUED | factura proveedor conciliada o devengo por política | reconciliation |
| ACCRUED | PAYABLE | fondos disponibles + recon OK | reconciliation |
| PAYABLE | PAID | confirmación del payment provider | payout confirmado |
| PAYABLE | DISPUTED | disputa abierta | usuario o regla |
| DISPUTED | estado origen | resolución a favor | workflow disputas |
| PAID | CLAWBACK | reverso posterior aprobado | adjustment |
| cualquiera | ADJUSTED | ajuste aprobado | nueva entrada, jamás mutación |

Guard universal (ver doc de eventos y consistencia):

```sql
UPDATE commission
SET status = 'PAYABLE'
WHERE id = $1 AND status = 'ACCRUED'
```

Ninguna transición ocurre sin verificar su predecesor bajo la misma fila.

---

# 3. Cuándo se contabiliza: posting policy

Los asientos T1-T2 del doc de ledger se mostraron "al calcular". Contablemente, el ingreso se devenga cuando se gana, no cuando se estima. Política explícita y configurable:

```yaml
settlement:

  posting_policy: ON_CONFIRMATION
  # ON_CALCULATION    asientos desde el cálculo (modo simple,
  #                   válido si el cliente contabiliza estimados)
  # ON_CONFIRMATION   devengo real al confirmar (recomendado)
```

Con `ON_CONFIRMATION`:

```text
ESTIMATED   sin asientos en el mayor; resultado operativo
            y proyecciones viven fuera del GL
CONFIRMED   se registran T1/T2 (ingreso devengado + payable agente)
ACCRUED     sin efecto extra si ya se devengó
PAID        se registran T3/T4 (movimientos de tesorería)
```

El resto del sistema no cambia: snapshots, trazabilidad e idempotencia aplican igual. Lo único que varía es el momento de escritura en el mayor.

---

# 4. Settlement runs: batches de pago

Un batch agrupa todo lo pagable de un beneficiario en una corrida.

## Selección

```sql
-- pseudocódigo de selección
SELECT beneficiario, SUM(net_payable)
FROM commissions
WHERE status = 'PAYABLE'
  AND organization_id = :org
GROUP BY beneficiario
HAVING SUM(net_payable) >= :min_payout_amount
```

Configuración:

```yaml
settlement:

  schedule:

    cadence: monthly
    cutoff_day: 25
    timezone: Europe/Madrid

  min_payout_amount: "50.00"
  # debajo del umbral: acumula para la siguiente corrida

  clawback_policy: OFFSET_NEXT_BATCH
  # OFFSET_NEXT_BATCH | STANDALONE_DEBIT
```

## Máquina de estados del batch

```text
DRAFT ──▶ PENDING_APPROVAL ──▶ APPROVED ──▶ EXECUTING ──▶ COMPLETED
                                  │             │
                    rechazo: vuelve│             ├──▶ COMPLETED_WITH_ERRORS
                    a DRAFT con    │             └──▶ FAILED
                    comentario

Estado fino por instrucción de pago:
CREATED → SUBMITTED → PROCESSING → PAID | FAILED | RETRYING
```

Propiedades:

```text
✓ El batch sobre umbral exige doble aprobación
  (maker-checker del doc de autorización)
✓ Cada instrucción porta idempotency_key propia:
  "batch:{batch_id}:payout:{payout_id}"
  → reintentos nunca pagan dos veces
✓ Estados parciales: un fallo no bloquea el resto del batch
✓ Ejecución protegida por guards de estado
  (cancelación vs payout en carrera, ver eventos y consistencia)
```

---

# 5. PaymentProvider abstraction

Interfaz coherente con la filosofía cloud-neutral:

```go
type PaymentProvider interface {

    ValidateBeneficiary(
        account BeneficiaryAccount,
    ) (ValidationResult, error)

    CreatePayout(
        instruction PayoutInstruction,
    ) (PayoutRef, error)

    GetStatus(
        ref PayoutRef,
    ) (PayoutStatus, error)

    Cancel(
        ref PayoutRef,
    ) error

}
```

`PayoutInstruction` incluye siempre:

```text
beneficiary_ref      referencia interna, nunca IBAN en claro
                     (los datos bancarios viven cifrados en el
                     almacén definido en protección de datos)
amount + currency    importe neto tras retenciones
idempotency_key
memo                 referencia visible para el receptor
fx_snapshot          si la divisa de pago difiere de la de cálculo
```

Implementaciones posibles:

```text
bank-file        CSV/domiciliaciones SEPA, ACH, SPEI (funciona
                  sin conexión externa; ideal self-hosted)
psp              Stripe Connect, Adyen for Platforms, ...
manual           export + marcado humano de pagado
custom-endpoint  API de tesorería del cliente enterprise
```

La confirmación de pago llega por polling de `GetStatus` o webhook del proveedor; ambos caminos terminan en el mismo guard de transición `PAYABLE → PAID`.

---

# 6. Retenciones y multi-divisa en el pago

Coherente con impuestos y multi-divisa:

```text
Comisión bruta agente:      127,50 EUR
Retención IRPF 15%:         -19,13 EUR
Neto a pagar:               108,37 EUR

Si el agente cobra en MXN:
conversión EUR → MXN con rate de fecha de pago,
snapshot propio, diferencia de redondeo a FX gain/loss.
La retención se declara en la jurisdicción del pagador;
el certificado al receptor se emite con el batch.
```

---

# 7. Disputas

Una comisión en cualquier estado pre-PAID puede congelarse:

```text
Apertura:
  motivo obligatorio + evidencias adjuntas (vault, no core)
  estado → DISPUTED (freeze: no entra en batches)

Resolución posible:
  PROCEED   vuelve al estado origen, sigue el flujo normal
  ADJUST    ajuste aprobado → entradas nuevas en ledger,
            recalcula neto y continúa
  REJECT    anula → CANCELLED o CLAWBACK según estado
```

Toda transición de disputa es auditada y dispara webhooks (`commission.disputed`, ya previsto en arquitectura). Las disputas sobre batches ya en EXECUTING no detienen pagos individuales ya sometidos; afectan a futuras corridas.

---

# 8. Clawbacks: dinero que ya se fue

Comisión pagada de más, detectada después. Jamás se muta el pago original:

```text
Política OFFSET_NEXT_BATCH (default):

  Agente cobró de más:            -100,00 EUR
  Su payable del mes siguiente:    300,00 EUR
  Batch paga:                      200,00 EUR
  Ledger: entrada CLAWBACK -100 referenciando
  el payout original y su calculation

Política STANDALONE_DEBIT:

  Entrada negativa independiente,
  gestionada como cobro al receptor
```

El clawback hereda el trail completo: qué regla causó el exceso queda visible en el evaluation_trace del recálculo.

---

# 9. Webhooks del ciclo

```text
commission.confirmed
commission.accrued
commission.payable
commission.disputed
commission.resolved
settlement.batch.created
settlement.batch.approved
settlement.payout.paid
settlement.payout.failed
clawback.registered
```

Mismo envelope con `event_id`, `idempotency_key` y `schema_version` definido en arquitectura; nacen del outbox como todos los eventos.

---

# 10. Qué NO construir

```text
✗ Entidad de dinero propia: la plataforma mueve referencias,
  el movimiento real lo ejecuta banco/PSP del cliente
✗ Cambio de divisa especulativo o custodia de saldos
✗ Módulo AR/AP completo de facturación
✗ KYC/KYB propio: se delega al PSP o se importa el resultado
✗ Reintento infinito de pagos fallidos: política acotada + DLQ
```

Posicionamiento:

> El motor decide quién cobra cuánto, lo congela, lo aprueba y ordena el pago. El dinero lo mueve la infraestructura financiera existente del cliente.

---

# 11. Fases

Qué entra en cada versión de este módulo está clasificado en:

> `../mvp/travel_commission_engine_alcance_v1.md` (sección 7)

Resumen: enums del ciclo de vida, transiciones manuales/API,
export CSV y marcado manual en v1;
batches, PaymentProvider y disputas en v2;
corridas automáticas multi-jurisdicción en v3.

---

# 12. Impacto en el resto de la arquitectura

```text
Ledger doble partida     posting_policy define CUÁNDO nacen T1-T4;
                         los movimientos de tesorería completan el ciclo
Eventos y consistencia   guards de estado + idempotencia de instrucciones;
                         pagos también viajan por outbox
Autorización             batches sobre umbral = cuatro ojos obligatorio
Impuestos                retenciones netadas en el payout; certificados por batch
Protección de datos      datos bancarios de beneficiarios viven en el vault,
                         nunca en el core ni en instrucciones
Jerarquía de reglas      recálculos por disputa usan el mismo ranking +
                         evaluation_trace del ajuste
§16   Webhooks           nuevos eventos del ciclo listados arriba
§24   Workflows          corridas programadas: candidato natural a Temporal
```

---

# 13. Resumen

```text
  EVENTOS (checkout · facturas · recon)
              │
              ▼
  ┌─────────────────────────────┐
  │ CICLO DE VIDA DE LA COMISIÓN│
  │ E→C→A→P→PAID                │
  │ guards por fila · disputas  │
  └──────────────┬──────────────┘
                 │ selección PAYABLE ≥ umbral
                 ▼
  ┌─────────────────────────────┐
  │ SETTLEMENT BATCH            │
  │ DRAFT → APPROVED → EXECUTING│
  │ maker-checker por umbral    │
  └──────────────┬──────────────┘
                 │ instrucciones idempotentes
                 ▼
  ┌─────────────────────────────┐
  │ PAYMENT PROVIDER            │
  │ bank-file | psp | manual    │
  └──────────────┬──────────────┘
                 │ confirmación
                 ▼
       LEDGER: T3/T4 · PAID
       clawbacks por offset
```

Principios finales:

> El estado avanza por hechos, no por fechas.

> Nadie cobra sin estado PAYABLE; nadie paga sin batch aprobado.

> Un reintento nunca paga dos veces.

> Corregir dinero entregado es escribir un clawback, jamás tocar el historial.

> Con este ciclo, la promesa inicial queda completa: configurar, calcular, liquidar, auditar y optimizar.
