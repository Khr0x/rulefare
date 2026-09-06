# Travel Commission Engine — Autorización y Aprobaciones

Documento complementario a:

> `travel_commission_engine_stack_arquitectura.md`

La propuesta técnica cubre autenticación (OIDC/SAML, sección 20) pero no autorización. En una plataforma donde cambiar configuración mueve dinero indirectamente, quién puede aprobar qué es parte del core, no del admin panel.

---

## El framing correcto

```text
Aprobar una regla de comisión = decidir un reparto financiero
Aprobar un rate FX manual    = fijar un tipo de cambio
Ejecutar un batch de pago    = mover dinero real
```

Este documento define:

```text
1. RBAC          Quién puede hacer qué, por organización
2. Maker-checker requiere doble aprobación (cuatro ojos)
3. Workflow      Máquina de estados de aprobación versionada
```

---

# 1. Principio fundamental

> La autenticación se delega; la autorización es propia.

```text
OIDC / SAML   Demuestra quién eres (IdP externo)
RBAC interno  Decide qué puedes hacer (plataforma)
```

Y la segunda mitad, no negociable:

> Nadie aprueba lo suyo. La segregación de funciones se impone en servidor.

---

# 2. Modelo RBAC

Roles y permisos separados; los roles son conjuntos de permisos:

```sql
role
---------------------------
id
organization_id   NULL = plantilla global
code              TEXT
name              TEXT

permission
---------------------------
id
code              TEXT
-- rules:create | rules:approve | rules:activate
-- ledger:read | ledger:export
-- settlements:execute | fx_rates:write
-- connectors:manage | org:admin | audit:read

role_permission
---------------------------
role_id
permission_id

user_role
---------------------------
subject_id        -- user o client de máquina
role_id
organization_id
granted_by
granted_at
```

## Roles base

```text
OrgAdmin        Gestiona usuarios y configuración de la org
                No aprueba reglas financieras por defecto
RuleEditor      Crea y edita borradores (maker)
RuleApprover    Revisa y aprueba (checker)
FinanceOps      Ejecuta settlements, gestiona FX, disputas
Auditor         Solo lectura, incluye export de auditoría
ConnectorMgr    Configura conectores y mappings
ServiceClient   Identidad de máquina con permisos mínimos
```

Principios:

```text
✓ Permisos granulares por acción, no por pantalla
✓ Un usuario puede tener varios roles
✓ Deny por defecto: sin rol explícito, no hay acceso
✓ Los roles ServiceClient nunca incluyen approve
```

---

# 3. Identidades de máquina

API-first implica que también los sistemas tienen roles:

```text
connector-erp     bookings:write
bi-exporter       ledger:read, audit:read
simulator-ui      calculations:simulate (sin ledger:read)
```

Misma tabla, mismos permisos, mismo aislamiento. Sin cuentas compartidas ni claves maestras.

---

# 4. Aislamiento multi-tenant en repositorio

El `organization_id` del §21 se convierte en garantía dura:

```text
1. Toda query pasa por el repositorio de tenant:
   el filtro org_id no es opcional ni manual.

2. El org_id resuelto del token JWT jamás se toma
   del cuerpo de la petición.

3. Tests de seguridad en CI que intentan cross-tenant
   access en cada endpoint: deben fallar todos.
```

Un error de aislamiento aquí es una fuga total entre clientes. Se trata como vulnerabilidad, no como bug.

---

# 5. Maker-checker (cuatro ojos)

Registro declarativo de qué acciones exigen doble aprobación:

```yaml
authorization:

  four_eyes:

    commission_rules:
      enabled: true

    tax_rules:
      enabled: true

    fx_manual_rates:
      enabled: true

    connector_configs:
      enabled: false

    settlement_batches:
      enabled: true
      threshold:
        amount: "10000.00"
        currency: EUR
        # bajo umbral: un aprobador
        # sobre umbral: dos aprobadores distintos del maker
```

Reglas del mecanismo, aplicadas en servidor:

```text
✓ El aprobador debe tener permiso rules:approve
✓ El aprobador debe ser distinto del maker
✓ Ningún rol acumula make + approve sobre la misma entidad
✓ ServiceClient no puede aprobar jamás
```

---

# 6. Workflow de aprobación

Máquina de estados sobre las entidades versionadas:

```text
                 ┌──────────────────────────────┐
                 │                              │
                 ▼                              │
  ┌───────┐  submit  ┌───────────┐  approve   ┌─┴────────┐ schedule
  │ DRAFT ├─────────▶│ IN_REVIEW ├───────────▶│ APPROVED ├─────────┐
  └───────┘          └─────┬─────┘            └──────────┘         │
     ▲                     │ reject                                ▼
     │  changes_requested  │                              ┌────────────┐
     └─────────────────────┘                              │   ACTIVE   │
                                                          └─────┬──────┘
                                                                │ nueva versión
                                                                ▼
                                                          ┌───────────┐
                                                          │ SUPERSEDED│
                                                          └───────────┘
```

Estados y significado:

```text
DRAFT           Editable por el maker
IN_REVIEW       Bloqueada para edición; visible al checker
CHANGES_REQUESTED  Vuelve a DRAFT con comentario obligatorio
APPROVED        Aprobada; espera vigencia
ACTIVE          En vigor según valid_from
SUPERSEDED      Reemplazada por versión posterior
RETIRED         Retirada sin reemplazo
```

Cada transición queda registrada:

```sql
approval_transition
---------------------------
id
entity_type     TEXT    -- CommissionRule | TaxRule | FxRateManual |
                        -- SettlementBatch | ConnectorConfig
entity_id
entity_version
from_status
to_status
actor_id
comment         TEXT NULL
created_at
```

Esto alimenta directamente la cadena de trazabilidad existente:

```text
cálculo → versión de regla → transiciones de aprobación → actores
```

La pregunta de auditoría "¿quién puso este 17%?" tiene respuesta completa.

---

# 7. Vigencia futura

Aprobar y activar son momentos distintos:

```text
Lunes      Se crea regla campaña de invierno
Martes     El checker la aprueba
1 enero    La regla entra en vigor sola (valid_from)
```

Beneficios:

```text
✓ Sin despliegues manuales ni ventanas de madrugada
✓ La aprobación puede revisarse con calma antes del efecto
✓ Coherente con el versionado temporal del §23
```

---

# 8. Break-glass

Las operaciones financieras reales necesitan salida de emergencia:

```yaml
authorization:

  break_glass:

    enabled: true

    allowed_roles: [finance_ops]

    max_window_minutes: 60

    requires_post_review: true
```

Comportamiento:

```text
1. Activación con motivo obligatorio
2. Todas las acciones marcadas y alertadas en tiempo real
3. Revisión post-hoc obligatoria por OrgAdmin o Auditor
4. Uso recurrente = señal de diseño incorrecto
```

---

# 9. Eventos e integraciones

```text
rule.submitted
rule.approved
rule.rejected
rule.activated
approval.pending        → notificación al checker asignado
breakglass.enabled
breakglass.used
```

Salida por webhooks y por canal de notificación. El workflow sin notificaciones no se usa; sin uso, vuelve todo al admin único, que es justo lo que este documento existe para evitar.

---

# 10. Qué NO construir

```text
✗ Servidor de identidad propio (ya decidido: IdP externo)
✗ Motor de workflows genérico (BPM) para esto
  — una tabla de transiciones basta durante años
✗ ABAC completo en la v1
  — scopes JSONB en user_role cubren casos especiales
✗ Jerarquías de aprobación multinivel complejas
  — umbral + segundo aprobador cubre el 95% real
✗ Permisos por UI (ocultar botones no es autorización)
```

---

# 11. Fases

Qué entra en cada versión de este módulo está clasificado en:

> `../mvp/travel_commission_engine_alcance_v1.md` (sección 7)

Resumen: en v1 solo API key por organización, un administrador
y audit log de cambios de reglas; RBAC completo, maker-checker
y break-glass en v2; scopes avanzados y auditoría certificable en v3.

---

# 12. Impacto en el resto de la arquitectura

```text
§16   Webhooks            eventos de aprobación como primera clase
§20   Autenticación      OIDC/SAML entrega identidad; aquí vive el permiso
§21   Multi-tenant       aislamiento forzado en repositorio + tests CI
§23   Versionado         toda versión lleva su trail de aprobación
§26   AI layer           reglas sugeridas por IA entran igual:
                         DRAFT → IN_REVIEW → APPROVED; la IA nunca activa
Impuestos               tax_rule exige checker desde su nacimiento
Ledger                  batches de payout sobre umbral exigen doble visto bueno
Agregación              definiciones de agregados también versionan con flujo
```

---

# 13. Resumen

```text
        IDENTIDAD (IdP externo)
                │ OIDC / SAML
                ▼
        ┌───────────────┐
        │ RBAC interno  │  ¿qué puedes hacer?
        └───────┬───────┘
                │
    ┌───────────┼─────────────┐
    ▼           ▼             ▼
 lectura    escritura    mutación financiera
 (audit)    (maker)          │
                             ▼
                    ┌─────────────────┐
                    │ MAKER - CHECKER │  ¿quién más lo viste?
                    │ cuatro ojos     │
                    └────────┬────────┘
                             ▼
                    ┌─────────────────┐
                    │ WORKFLOW        │  DRAFT → REVIEW → ACTIVE
                    │ + vigencia      │  actor · timestamp · comment
                    └────────┬────────┘
                             ▼
                       AUDIT TRAIL
                 cálculo → regla → aprobador
```

Principios finales:

> Autenticación delegada, autorización propia.

> Nadie aprueba lo suyo, jamás.

> Toda mutación financiera tiene nombre, fecha y segundo par de ojos.

> Si cambiar una regla no deja rastro de quién la aprobó, la plataforma no es auditable.
