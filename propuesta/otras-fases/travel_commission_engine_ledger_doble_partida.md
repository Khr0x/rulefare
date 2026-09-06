# Travel Commission Engine — Ledger en doble partida

Documento complementario a:

> `travel_commission_engine_stack_arquitectura.md`

La sección 22 del documento de arquitectura define un ledger append-only correcto como historial, pero insuficiente como sistema financiero. Este documento lo completa.

---

## El problema

Un log unilateral:

```text
CALCULATED   +€425
ADJUSTMENT   -€25
BONUS        +€50
```

Registra qué ocurrió, pero no puede responder preguntas esenciales:

```text
✗ ¿Todo cuadra? ¿Hay dinero aparecido o desaparecido?
✗ ¿Cuánto debemos a cada agente a día de hoy?
✗ ¿Cuánta comisión está pendiente de cobrar por proveedor?
✗ ¿Qué pasivos fiscales tenemos abiertos?
✗ Suma de saldos ¿coincide con el banco?
```

Sin doble partida, settlements y payments se construyen sobre arena: no hay forma de demostrar que la plataforma no ha perdido ni inventado dinero.

---

# 1. Principio fundamental

> Toda transacción tiene al menos dos entradas y siempre cuadra.

```text
Σ débitos = Σ créditos
```

En cada transacción, sin excepción. El ledger rechaza cualquier escritura que no cumpla el invariante.

Segundo principio, heredado del documento de arquitectura:

> El ledger es append-only. Corregir es escribir un reverso, jamás modificar.

---

# 2. Modelo de datos

## Cuentas

```sql
account
---------------------------
id
organization_id
code                TEXT        -- código jerárquico
name                TEXT
type                TEXT        -- ASSET | LIABILITY | INCOME | EXPENSE | SYSTEM
currency            CHAR(3)     -- cada cuenta opera en UNA divisa
party_id            UUID NULL   -- subledger de contraparte (agente, proveedor)
parent_account_id   UUID NULL
status              TEXT
created_at
```

Propiedades clave:

```text
✓ Los saldos son SIEMPRE por divisa. Nunca se netean divisas.
✓ Las cuentas con party_id forman subledgers
  (una cuenta corriente por agente, proveedor, franquicia).
✓ El plan de cuentas es configurable por organización,
  con una plantilla por defecto razonable.
```

## Transacciones y entradas

```sql
ledger_transaction
---------------------------
id
organization_id
reference_type      TEXT    -- CALCULATION | SETTLEMENT | PAYOUT |
                            -- ADJUSTMENT | REVERSAL | CONVERSION
reference_id        UUID
memo                TEXT
idempotency_key     TEXT UNIQUE
fx_snapshot         JSONB NULL
effective_date      DATE
created_at
created_by          TEXT    -- system | user_id

ledger_entry
---------------------------
id
transaction_id      UUID
account_id          UUID
direction           TEXT    -- DEBIT | CREDIT
amount              NUMERIC(18,4) CHECK (amount > 0)
currency            CHAR(3)
base_amount         NUMERIC(18,4)  -- equivalente en divisa base
created_at
```

Notas de diseño:

```text
✓ amount es siempre positivo; direction porta el signo.
  Elimina toda una clase de bugs de signo.
✓ base_amount permite verificar el cuadre global y reportar
  en divisa base sin reconversiones (ver multi-divisa).
✓ idempotency_key derivada del evento de origen:
  ej. "calc:{calculation_id}" garantiza que un reproceso
  nunca duplique asientos.
```

---

# 3. Plan de cuentas ilustrativo

```text
ASSET
  1000  Bank / PSP                          (por divisa)
  1200  Receivable · Suppliers              (subledger)

LIABILITY
  2000  Payable · Agents                    (subledger)
  2010  Payable · Franchises                (subledger)
  2020  Payable · Affiliates                (subledger)
  2200  VAT collected                       (pasivo fiscal)
  2210  Withholding payable                 (pasivo fiscal)

INCOME
  4000  Commission revenue
  4010  Platform fee revenue

EXPENSE
  5000  Agent commission expense
  5100  Rappel / bonus expense

SYSTEM
  9000  FX gain / loss
  9900  Suspense                            (debe existir saldo 0)
```

La cuenta `Suspense` existe para detectar errores: si algo aterriza ahí, hay un bug o una operación sin clasificar. Debe permanecer a cero.

---

# 4. Ejemplo end-to-end

Los mismos números usados en los documentos de impuestos y multi-divisa:

```text
Reserva:               2.500 USD → 2.308,50 EUR
Comisión calculada:      392,44 EUR
IVA 21%:                  82,41 EUR
Comisión agente:         127,50 EUR
Retención IRPF 15%:       19,13 EUR
Neto agente:             108,37 EUR
```

## T1 · Comisión devengada (al calcular)

```text
DEBIT   1200 Receivable · Hotelbeds      474,85
CREDIT  4000 Commission revenue          392,44
CREDIT  2200 VAT collected                82,41

Cuadre:  474,85 = 392,44 + 82,41  ✓
```

## T2 · Derecho del agente devengado

```text
DEBIT   5000 Agent commission expense    127,50
CREDIT  2000 Payable · agent_123         108,37
CREDIT  2210 Withholding payable          19,13

Cuadre:  127,50 = 108,37 + 19,13  ✓
```

## T3 · Cobra el proveedor

```text
DEBIT   1000 Bank EUR                    474,85
CREDIT  1200 Receivable · Hotelbeds      474,85
```

## T4 · Pago al agente

```text
DEBIT   2000 Payable · agent_123         108,37
CREDIT  1000 Bank EUR                    108,37
```

Tras las cuatro transacciones:

```text
Saldo 2000 Payable · agent_123     0,00   ✓ liquidado
Saldo 2200 VAT collected          82,41   pendiente de declarar
Saldo 2210 Withholding            19,13   pendiente de ingresar
Saldo 1000 Bank                 +366,48   coincide con realidad
```

Esto es exactamente la respuesta a la pregunta fundacional del producto: quién cobra cuánto, por qué, y ahora también con validez contable.

---

# 5. Multi-divisa en el ledger

Reglas:

```text
1. Cada entrada registra su divisa nativa y su equivalente
   en base_currency con referencia al fx snapshot.

2. Las transacciones operativas son mono-divisa
   (un cobro, un pago, un ajuste).

3. La conversión entre divisas es una transacción explícita:

   CONVERSION
   DEBIT   1000 Bank USD     500,00 USD
   CREDIT  1000 Bank EUR     461,70 EUR   (rate 0.9234)

   El cuadre se verifica sobre base_amount;
   la diferencia de redondeo aterriza en 9000 FX gain/loss.
```

Nunca existe una cuenta con saldo mezclado de divisas.

---

# 6. Atomicidad e idempotencia

Escritura del ledger, reglas innegociables:

```text
1. Transaction + entries + cambio de estado de negocio
   en LA MISMA transacción de PostgreSQL.

2. Validación de cuadre antes de INSERT.
   Rechazo explícito, nunca autocuadre silencioso.

3. idempotency_key única por evento de origen.
   Reprocesar una calculation no duplica asientos.

4. Sin escrituras fuera del servicio de ledger.
   Ningún módulo hace UPDATE directo sobre tablas contables.
```

Estas garantías comparten raíz con el patrón transaccional outbox para eventos: si se resuelve ese punto de la arquitectura, el mismo mecanismo sirve aquí.

---

# 7. Correcciones: solo por reverso

Error en un cálculo ya contabilizado:

```text
✗ NUNCA:  UPDATE ledger_entry SET amount = ...

✓ SIEMPRE: nueva transacción REVERSAL
           que referencia la original
           y reproduce sus entradas invertidas
```

Ejemplo: clawback de una comisión pagada de más:

```text
REVERSAL OF T2' (comisión original era menor)

DEBIT   2000 Payable · agent_123          25,00
CREDIT  5000 Agent commission expense     25,00

referencia: original_transaction_id
motivo: CLAWBACK booking #93212
```

El historial completo queda intacto y auditable, incluido el error.

---

# 8. Saldos y reporting

## Saldos

El saldo de una cuenta se deriva de sus entradas y se materializa incrementalmente:

```sql
account_balance
---------------------------
account_id
period_key        TEXT     -- 2026-08
opening_balance   NUMERIC(18,4)
debit_total       NUMERIC(18,4)
credit_total      NUMERIC(18,4)
closing_balance   NUMERIC(18,4)
rebuildable       BOOLEAN DEFAULT TRUE
```

Misma filosofía que los agregados: materializado reconstruible por replay de entradas.

## Salidas estándar

```text
Sumas y saldos (trial balance) por periodo
Libro diario filtrable (por reserva, agente, proveedor)
Antigüedad de saldos de payables/receivables
Extracto de cuenta por contraparte
Export de asientos hacia ERP contable
  (mapeo de plan de cuentas vía configuración,
  mismo espíritu que el mapping engine de integraciones)
```

---

# 9. Qué NO construir

```text
✗ Un ERP completo (facturas de compra/venta completas,
  activos fijos, amortizaciones, nóminas)
✗ Presentación de impuestos
✗ Conciliación bancaria automática avanzada en la v1
  (llega con reconciliation, fase 2)
✗ Múltiples planes contables complejos por país en el core
```

Posicionamiento correcto:

> La plataforma es un subledger financiero especializado en comisiones y liquidaciones. El ERP del cliente sigue siendo el sistema contable oficial; recibimos y entregamos asientos limpios.

---

# 10. Fases

Qué entra en cada versión de este módulo está clasificado en:

> `../mvp/travel_commission_engine_alcance_v1.md` (sección 7)

Resumen: en v1 solo existe el log plano de movimientos append-only
y saldos derivados por beneficiario; la doble partida completa
llega en v2 junto a settlements automáticos;
particionado masivo y conciliación bancaria en v3.

---

# 11. Impacto en el resto de la arquitectura

```text
§5bis  Multi-divisa      cuentas por divisa + base_amount + snapshots
Impuestos                2200/2210 convierten pasivos fiscales en saldos reales
Agregación               ADJUSTMENT de period rules = transacciones cuadradas
§15    Mapping engine    mismo patrón para mapear plan de cuentas del cliente
§16    Webhooks          eventos: ledger.transaction.created, account.statement.ready
§24    Workflows         cierre contable mensual = worker batch programado
Alcance v1             log plano + saldos en v1; doble partida en v2
                         (ver alcance_v1)
```

---

# 12. Resumen

```text
ANTES (apartado 22)

  CALCULATED  +425 ──▶ historial plano
                       sin cuadre, sin saldos, sin partidas



DESPUÉS (propuesta)

              TRANSACTION (siempre cuadra)
                      │
        ┌─────────────┼──────────────┐
        ▼             ▼              ▼
    ENTRY DR      ENTRY CR       ENTRY CR
        │             │              │
        └─────────────┴──────────────┘
                      │
        ACCOUNTS por divisa y contraparte
                      │
        ┌─────────────┼──────────────┐
        ▼             ▼              ▼
     BANKS       SUBLEDGERS      PASIVOS FISCALES
                 agentes ·        IVA · retenciones
                 proveedores
                      │
                      ▼
        Trial balance · Extractos · Export ERP
```

Principios finales:

> Débito igual a crédito, siempre, o la escritura se rechaza.

> Cada entrada nace trazable hasta su calculation y su regla.

> Corregir es escribir; jamás borrar ni mutar.

> Saldo por divisa, saldo por parte, saldo verificable contra el banco.
