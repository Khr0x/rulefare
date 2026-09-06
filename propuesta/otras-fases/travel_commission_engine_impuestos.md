# Travel Commission Engine — Impuestos y Fiscalidad

Documento complementario a:

> `travel_commission_engine_stack_arquitectura.md`

Las comisiones son eventos económicos y, como tales, tienen consecuencias fiscales. La propuesta técnica actual no contempla este módulo. Este documento lo define.

---

## Objetivo

Que cada comisión, pago y liquidación generada por la plataforma:

- Incluya su tratamiento fiscal correcto según los países de las partes
- Sea trazable hasta la regla fiscal aplicada
- Sea reproducible históricamente (versionado + snapshots)
- Genere los datos necesarios para facturar y reportar
- Sin convertir la plataforma en un motor fiscal genérico

---

# 1. Principio fundamental

> Los impuestos nunca se mezclan con la comisión.

Toda cantidad se representa siempre desglosada:

```text
base          Importe de la comisión sin impuestos
tax           Impuesto calculado sobre esa base
total         base + tax
withholding   Retención practicada sobre la base
net_payout    Lo que realmente se transfiere
```

Ejemplo:

```text
Comisión (base):            100,00
IVA 21%:                    +20,00
Total factura:              120,00

Retención IRPF 15%:         -15,00
Importe neto a percibir:    105,00
```

La comisión es siempre 100. El resto son líneas derivadas, registradas y trazables.

---

# 2. Alcance: qué impuestos sí y cuáles no

## Sí (impuesto sobre la comisión)

```text
VAT / GST / IVA          Impuesto indirecto sobre el servicio
Sales tax                Según jurisdicción
Withholding / retención  IRPF, ISR, retenciones en origen
Digital services tax     Cuando aplique a fees de plataforma
```

## No (fuera del motor)

Los impuestos sobre el viaje mismo:

```text
IVA hotelero, tasas aéreas, impuestos de salida,
city tax, impuestos locales de hospedaje
```

Esos viajan dentro del dato de reserva como parte del precio y no son responsabilidad del motor de comisiones. Llegan ya incluidos en los importes del modelo canónico.

Frontera clara:

> El motor fiscaliza comisiones, fees y pagos entre partes. No fiscaliza viajes.

---

# 3. Los tres flujos con implicaciones fiscales

No existe "un impuesto". Existen tres flujos distintos con tratamientos diferentes:

```text
FLUJO A: INGRESO POR COMISIONES
───────────────────────────────
Proveedor paga comisión a agencia/plataforma

Preguntas fiscales:
¿Quién factura? ¿Autofactura?
¿IVA repercutido o inversión del sujeto pasivo?
¿Hay retención en origen del proveedor?


FLUJO B: PAGO A AGENTES Y RED
─────────────────────────────
Plataforma/agencia paga a agente, franquicia o afiliado

Preguntas fiscales:
¿Qué retención practica el pagador?
¿Persona física o jurídica el receptor?
¿Convenio de doble imposición aplicable?
Certificados de retención para el receptor


FLUJO C: FEE DE PLATAFORMA
──────────────────────────
El SaaS cobra fee o suscripción a sus clientes

Preguntas fiscales:
¿País de establecimiento del cliente?
¿B2B o B2C?
¿Reverse charge intra-UE?
```

El módulo fiscal debe modelar los tres flujos desde el principio, aunque el primer cliente solo active uno.

---

# 4. Determinación de jurisdicción

El tratamiento fiscal depende de dónde están establecidas las partes, no de dónde ocurre la venta.

Datos requeridos de cada parte:

```text
país de establecimiento
tipo de entidad          BUSINESS | INDIVIDUAL
identificación fiscal    NIF/CIF, RFC, EIN, ...
régimen                  general | simplificado | exento
certificados             residencia fiscal, W-8, W-9
```

Matriz ilustrativa de tratamientos:

```text
Payer   Payee   Flujo   Tratamiento típico
─────────────────────────────────────────────────
ES      ES      A/B/C   IVA repercutido + retención local
ES      FR      B2B     Reverse charge (sin IVA del emisor)
ES      MX      B       Retención en origen + convenio
US      US      B       Sin retención, reporting 1099
US      DE      B       Withholding 30% o tasa de convenio
```

La matriz completa vive en configuración versionada por jurisdicción, nunca en código.

---

# 5. Modelo de datos

## Regla fiscal (versionada, igual que las comisiones)

```sql
tax_rule
---------------------------
id
organization_id
jurisdiction        -- ISO país o región fiscal
tax_type            -- VAT | GST | SALES_TAX | WITHHOLDING | DST
flow                -- A_COMMISSION_INCOME | B_AGENT_PAYOUT | C_PLATFORM_FEE
treatment           -- CHARGE | REVERSE_CHARGE | EXEMPT | WITHHOLD | REPORT_ONLY
rate                NUMERIC(7,6)
base                -- COMMISSION_BASE | FEE_BASE | ...
condition           -- expresión CEL opcional
valid_from          TIMESTAMPTZ
valid_to            TIMESTAMPTZ
version
created_by
created_at
status
```

## Perfil fiscal de una parte

```sql
party_tax_profile
---------------------------
id
organization_id
party_id            -- agent | franchise | affiliate | supplier
entity_kind         -- BUSINESS | INDIVIDUAL
establishment_country
tax_id
tax_scheme          -- GENERAL | SIMPLIFIED | EXEMPT
treaty_country      -- si aplica convenio
certificate_on_file BOOLEAN
verified_at
```

Sin perfil fiscal completo, el tratamiento es conservador y configurable:

```yaml
tax:

  missing_tax_profile: warn
  # ignore | warn | block
```

---

# 6. Tratamientos estándar

Un conjunto cerrado de tratamientos cubre la práctica real:

```text
CHARGE            Se factura impuesto (repercutido)
REVERSE_CHARGE    No se factura; el receptor autoliquida
EXEMPT            Operación exenta, con motivo registrado
WITHHOLD          Se retiene sobre la base al pagar
REPORT_ONLY       Sin cálculo, pero se registra para reporting
```

Cada línea de impuesto resultante referencia siempre:

```text
tratamiento + tarifa + versión de regla + jurisdicción
```

---

# 7. Extensión del CalculationResult

El resultado de cálculo incorpora líneas fiscales:

```json
{
  "commission": "392.44",

  "tax_lines": [
    {
      "type": "VAT",
      "flow": "C_PLATFORM_FEE",
      "jurisdiction": "ES",
      "treatment": "CHARGE",
      "rate": "0.210000",
      "base": "COMMISSION_BASE",
      "amount": "82.41",
      "rule_version": 12
    }
  ],

  "payout_lines": [
    {
      "beneficiary": "agent_123",
      "gross": "127.50",
      "tax_lines": [
        {
          "type": "WITHHOLDING",
          "flow": "B_AGENT_PAYOUT",
          "jurisdiction": "ES",
          "treatment": "WITHHOLD",
          "rate": "0.150000",
          "amount": "-19.13",
          "rule_version": 5
        }
      ],
      "net": "108.37"
    }
  ]
}
```

---

# 8. Determinismo y snapshots

Mismo criterio que reglas de comisión y FX:

```text
mismo booking
+
misma versión de reglas de comisión
+
mismo snapshot de FX
+
mismo snapshot de reglas fiscales
=
mismo resultado completo
```

Una subida del IVA no debe alterar cálculos históricos ya emitidos. Las nuevas operaciones usan la nueva versión con su vigencia temporal.

---

# 9. Ledger e impuestos

El ledger registra los impuestos como partidas propias, nunca incrustadas en la comisión:

```text
COMMISSION_EARNED      +392.44   ingreso devengado
VAT_COLLECTED           +82.41   pasivo fiscal (repercutido)
AGENT_PAYABLE          -108.37   deuda neta con el agente
WHT_WITHHELD            +19.13   pasivo fiscal (retención practicada)
```

Esto permite responder directamente:

```text
¿Cuánto IVA hemos repercutido este trimestre?
¿Cuántas retenciones tenemos pendientes de ingresar?
¿Qué certificados de retención emitir por agente?
```

Requiere que el ledger soporte doble partida con cuentas de pasivo fiscal. Ver sección de ledger en el documento de arquitectura.

---

# 10. Reporting fiscal

La plataforma no presenta impuestos. Produce los datos para quien los presenta.

Salidas por jurisdicción y periodo:

```text
Resumen de IVA repercutido / soportado
Detalle de retenciones practicadas por parte
Base imponible por país y tipo de operación
Listado de operaciones intracomunitarias
Pagos a terceros para reporting (1099, equivalentes)
```

Formatos:

```text
Export CSV / JSON estándar
Export hacia ERP contable
Connector de e-invoicing (fase posterior)
```

Nota: varios países exigen factura electrónica certificada (CFDI en México, regulaciones de software de facturación certificado en España). Es un problema de integración y de producto por país, no del core. El core garantiza que los datos estén completos y trazables.

---

# 11. Configuración

```yaml
tax:

  enabled: true

  default_jurisdiction: ES

  missing_tax_profile: warn
  # ignore | warn | block

  rounding: HALF_UP

  require_certificate_for_treaty: true

  reporting:

    period: monthly
    exporters:
      - csv
      - erp-generic
```

Por organización, coherente con multi-tenant.

---

# 12. Qué NO construir

Disciplina de alcance:

```text
✗ Motor fiscal genérico multi-industria
✗ Cálculo de impuestos del viaje (vuelen fuera del motor)
✗ Presentación telemática de modelos fiscales
✗ Asesoría fiscal embebida en decisiones automáticas
✗ Lógica fiscal hard-codeada en el core
```

Todo tratamiento vive en `tax_rule` versionado y configurable. El core solo ejecuta.

---

# 13. Fases

Qué entra en cada versión de este módulo está clasificado en:

> `../mvp/travel_commission_engine_alcance_v1.md` (sección 7)

Resumen: el módulo fiscal completo es v3. En v1 solo existe la
estructura preparada: importes con divisa y desglose base/impuesto
en los resultados, sin cálculo fiscal activo.

Igual que con la multi-divisa: añadir fiscalidad sobre datos históricos sin esquema preparado es de los refactorings más caros que existen. El esquema nace preparado aunque solo se active una jurisdicción.

---

# 14. Impacto en el resto de la arquitectura

```text
§5   Dinero               tax_lines con Decimal, nunca float
§5bis FX                  importes fiscales también en par {valor, divisa}
§14   Modelo canónico     party_tax_profile asociado a cada parte
§22   Ledger              partidas de pasivo fiscal separadas
§23   Versionado          tax_rule idéntica en espíritu a CommissionRule
§28   Stack               nueva fila: módulo fiscal versionado
Alcance v1            fuera del núcleo; esquema preparado,
                         activo en v3 (ver alcance_v1)
```

---

# 15. Resumen

```text
                    COMMISSION CORE
                          │
                          ▼
                 ┌─────────────────┐
                 │ CALCULATION     │
                 │ ENGINE          │
                 └────────┬────────┘
                          │
                          ▼
                 ┌─────────────────┐
                 │ TAX MODULE      │
                 │                 │
                 │ tax_rules       │
                 │ tax_profiles    │
                 │ treatments      │
                 │ snapshots       │
                 └────────┬────────┘
                          │
            ┌─────────────┼─────────────┐
            ▼             ▼             ▼
         LEDGER      SETTLEMENT     REPORTING
      pasivos fiscales  netos      exports por
                     a pagar       jurisdicción
```

Principios:

> Base, impuesto y total siempre desglosados.

> Toda línea fiscal es trazable hasta su regla y versión.

> El motor ejecuta; la configuración decide.

> Datos completos para facturar y reportar; presentarlos no es problema del core.
