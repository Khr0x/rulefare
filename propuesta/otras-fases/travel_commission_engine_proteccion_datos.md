# Travel Commission Engine — Protección de datos y RGPD

Documento complementario a:

> `travel_commission_engine_stack_arquitectura.md`

El ledger inmutable y el derecho al olvido parecen chocar de frente. Este documento resuelve el conflicto por diseño, no por parches de cumplimiento.

---

## El conflicto aparente

```text
RGPD art. 17         Derecho al borrado de datos personales
Ledger append-only   Prohibido modificar o eliminar registros
Contabilidad         Obligación legal de conservar 6-10 años
```

Si una reserva contiene el nombre y email de un viajero, ¿cómo se cumple el borrado sobre un historial financiero intocable?

---

# 1. La clave jurídica

> El art. 17.3(b) del RGPD exime del borrado cuando la conservación responde a una obligación legal.

Los registros contables y fiscales pueden conservarse con datos personales. Pero esa eximición exige dos cosas:

```text
1. Minimización: solo los datos personales imprescindibles
2. Protección: los que se conservan deben estar asegurados
```

La estrategia correcta no es "borrar contra el ledger". Es diseñar para que casi ningún dato personal entre, cifrar el imprescindible y poder destruirlo sin tocar filas.

---

# 2. Principio fundamental

> El mejor dato personal es el que nunca entró.

Jerarquía de decisiones, en orden:

```text
1. NO RECOPILAR    El dato no hace falta para comisiones
2. SEUDONIMIZAR    Referencia opaca al sistema del cliente
3. CIFRAR          Envelope encryption con shredding
4. RETENER/BORRAR  Schedule automático por jurisdicción
```

---

# 3. Minimización: customer como referencia opaca

Las comisiones son flujos B2B entre organización, agentes y proveedores. El viajero casi nunca hace falta.

Modelo canónico corregido:

```json
{
  "customer": {
    "ref": "ext-cust-98213",
    "segment": "premium"
  }
}
```

```text
✗ name, email, phone, passport del viajero
✓ ref opaca asignada por el sistema origen
✓ atributos derivados no identificadores (segmento, país)
```

La plataforma sabe que la reserva existe, cuánto vale y qué regla aplica. Nunca necesita saber quién viaja.

---

# 4. Metadata JSONB: el vector de fuga

`metadata` es libre por diseño. En la práctica, alguien meterá nombres de pasajeros, teléfonos o DNI ahí dentro.

Defensa en tres capas:

```yaml
privacy:

  metadata_validation:

    mode: warn
    # off | warn | reject según organización

    patterns:
      - email
      - phone
      - iban
      - national_id_heuristics
```

```text
Capa 1  Validación de esquema con detección heurística de PII
Capa 2  Regla contractual: metadata sin datos personales
Capa 3  Retention corto específico para metadata
        (independiente del ledger financiero)
```

---

# 5. Clasificación e inventario de datos

Todo campo del modelo canónico lleva clase:

```text
FINANCIAL     Importes, divisas, reglas aplicadas
TECHNICAL     IDs externos, timestamps, versiones
PERSONAL_REF  Referencias opacas a terceros sistemas
PERSONAL      Datos identificativos reales (excepción justificada)
```

Inventario vivo generado del esquema, no de un documento Word. Es la base del registro de actividades de tratamiento (art. 30) y de responder DSARs sin cacería manual.

---

# 6. Crypto-shredding

Para los datos personales que sí existen — agentes, titulares de franquicias, contactos de proveedores — el mecanismo compatible con append-only es el cifrado por sobre:

```text
                    KMS / Vault
                        │
        ┌───────────────┴───────────────┐
        │  KEK sujeto_A     KEK sujeto_B│
        └───────┬───────────────┬───────┘
                │ envuelve      │
        ┌───────▼──┐    ┌───────▼───┐
        │ DEK_a1..n│    │ DEK_b1..n │
        └───────┬──┘    └──────┬────┘
                │ cifra        │
        Registros cifrados en tablas append-only
        (las filas jamás se modifican)

BORRADO = destroy(KEK sujeto_A)
→ filas intactas · ciphertext irrecuperable
→ retención legal cumplida · derecho al olvido efectivo
```

Esquema:

```sql
pii_record
---------------------------
id
subject_key          TEXT   -- pseudónimo estable del sujeto
field_name           TEXT
ciphertext           BYTEA  -- cifrado con DEK
wrapped_dek          BYTEA  -- DEK envuelta por KEK del sujeto
kek_id               TEXT
context_ref          UUID   -- ej. agent_id, sin datos en claro
created_at
```

Interfaz coherente con la filosofía cloud-neutral:

```go
type KMSProvider interface {

    Wrap(dek []byte, kekID string) ([]byte, error)

    Unwrap(wrapped []byte, kekID string) ([]byte, error)

    Destroy(kekID string) error

}
```

Implementaciones posibles:

```text
vault-transit   HashiCorp Vault (self-hosted)
aws-kms         Para despliegues SaaS en AWS
custom-endpoint KMS interno del cliente enterprise
```

Decisión temprana obligatoria: aplicar crypto-shredding sobre histórico en claro posteriormente significa re-cifrar todo y auditar el periodo expuesto.

Nota importante: los perfiles fiscales (`party_tax_profile`) contienen NIF/RFC de personas físicas. Ese documento debe leerse junto a este: los campos identificativos viven cifrados aquí, el perfil guarda la referencia.

---

# 7. Almacén PII separado

Si una funcionalidad real requiere datos en claro — gestión de disputas, KYC de pagos — viven en un almacén propio, no en el core:

```text
CORE FINANCIERO                 PII VAULT
solo referencias y cifrado      datos en claro cifrados
append-only                     borrable por diseño
retención contable larga        retención corta por finalidad
        ▲                              │
        └──────── context_ref ─────────┘
```

Propiedades:

```text
✓ Borrar del vault no toca el ledger ni los cálculos
✓ El vault puede estar desplegado aparte o desactivado
✓ Sin vault, la plataforma simplemente no tiene PII en claro
```

---

# 8. Retención como configuración

```yaml
privacy:

  retention_profile: es_general
  # es_general | de_handelsgesetzbuch | custom

  schedules:

    calculation_financial_data: 120mo
    # obligación contable; sobrevive a cualquier borrado

    booking_metadata: 24mo

    pii_vault_contacts: 36mo

    audit_events: 60mo

  purge_job:
    schedule: daily_0300_utc
    dry_run_first: true
```

El job de purga distingue tres operaciones:

```text
DELETE   fila fuera de retención, sin valor contable
SHRED    destruir KEK; la fila contable permanece
ANONYMIZE sustituir por valores irreversibles no personales
```

---

# 9. Derechos RGPD operativos

```text
ACCESO (art. 15)
  Export JSON del sujeto: inventario + vault + refs
  Generado por tooling, no por consultas manuales

BORRADO (art. 17)
  Shred de KEKs + purga de vault + anonimización de refs
  Registro del propio evento de borrado (ver sección 10)

PORTABILIDAD (art. 20)
  Mismo export del acceso
```

SLA interno objetivo: DSAR resuelto sin intervención humana salvo aprobación.

---

# 10. Auditoría del propio borrado

El borrado también deja rastro, sin contenido:

```json
{
  "event": "privacy.erasure.executed",
  "subject_key": "subj_8f31...",
  "shredded_keks": 1,
  "vault_records_purged": 4,
  "refs_anonymized": 12,
  "requested_by": "dsar_2026_0417",
  "approved_by": "dpo_user",
  "at": "2026-08-23T09:12:00Z"
}
```

Se demuestra que hubo borrado sin reconstruir qué se borró.

---

# 11. Qué NO construir

```text
✗ Motor de consentimiento genérico (no hay marketing aquí)
✗ Cifrado de todo el core "por si acaso"
  (costo enorme, ganancia mínima si no entra PII)
✗ Borrado físico sobre tablas contables
✗ Anonimización reversible mal llamada anonimización
✗ Gestor documental de contratos con PII en la v1
```

---

# 12. Fases

Qué entra en cada versión de este módulo está clasificado en:

> `../mvp/travel_commission_engine_alcance_v1.md` (sección 7)

Resumen: la v1 ya nace sin PII (customer_ref opaco, metadata warn,
sin vault); KMS y crypto-shredding en v2-v3 junto a disputas y pagos;
certificaciones y perfiles multi-jurisdicción en v3.

---

# 13. Impacto en el resto de la arquitectura

```text
§14   Modelo canónico     customer como ref opaca; clases de datos
Impuestos               party_tax_profile: identificadores cifrados aquí
Ledger doble partida    las filas financieras no llevan PII;
                        sobreviven intactas a cualquier shred
Agregación              contribuciones referencian cálculos, no personas
§23   Versionado         eventos de erasure también auditados
§27   AI abstraction     extracción de contratos puede tocar PII:
                        va siempre vía vault, nunca al core
```

---

# 14. Resumen

```text
        DATO PERSONAL
             │
   ┌─────────▼──────────┐
   │ ¿Hace falta?       │
   └───┬────────────┬───┘
      NO           SÍ
       │            │
  no existe    ┌──▼───────────────┐
               │ ¿En claro basta? │
               └──┬────────────┬──┘
                 NO           SÍ (raro)
                  │            │
          customer_ref    PII VAULT cifrado
          en el core      borrable por diseño
                  │            │
                  └─────┬──────┘
                        ▼
              RETENCIÓN programada
              DELETE · SHRED · ANONYMIZE
                        │
                        ▼
              LEDGER INTACTO SIEMPRE
              (cumple contabilidad Y rgpd)
```

Principios finales:

> El mejor dato personal es el que nunca entró.

> Borrar es destruir claves, no filas.

> La retención legal y el olvido conviven cuando el cifrado media entre ambos.

> Todo tratamiento, incluido el borrado, deja auditoría.
