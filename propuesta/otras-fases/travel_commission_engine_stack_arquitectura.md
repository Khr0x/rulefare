# Travel Commission Engine — Stack y Arquitectura Técnica

> **Estado documental:** arquitectura objetivo y catálogo de opciones para fases posteriores; no describe el despliegue comprometido del MVP. Las referencias internas a NATS, microservicios, Kubernetes, self-hosting o etiquetas de versión antiguas quedan subordinadas a [Alcance v1](../mvp/travel_commission_engine_alcance_v1.md) y al [roadmap vigente](../mvp/roadmap.md), que para v1 fijan un monolito modular Go + PostgreSQL sin broker ni Kubernetes.

## Objetivo

Construir una plataforma de comisiones y revenue para el sector travel que sea:

- **API-first**
- Muy flexible
- Rápida
- Multi-tenant
- SaaS y self-hosted
- Compatible con entornos enterprise
- Integrable con sistemas existentes
- Adaptable a distintas bases de datos
- Desacoplada de proveedores cloud
- Preparada para incorporar IA
- Auditable y determinista en los cálculos financieros

La recomendación principal de stack es:

> **Go + PostgreSQL + OpenAPI + CEL + NATS JetStream + gRPC + Docker/Kubernetes + OpenTelemetry**

---

# 1. Principio arquitectónico principal

La plataforma debe controlar su propio modelo y su propia base de datos.

La flexibilidad para trabajar con distintas bases de datos, ERPs, CRMs, GDS, PMS y sistemas legacy se debe resolver mediante una **capa de conectores**.

No conviene intentar que todo el core funcione nativamente sobre PostgreSQL, Oracle, MSSQL, MySQL, DB2, MongoDB, etc.

Arquitectura conceptual:

```text
                         CLIENTES
                             │
          ┌──────────────────┼──────────────────┐
          │                  │                  │
      REST API           Webhooks           SDKs
          │                  │                  │
          └──────────────────┼──────────────────┘
                             │
                       API GATEWAY
                             │
                             ▼
                ┌────────────────────────┐
                │     CORE PLATFORM      │
                │          Go            │
                ├────────────────────────┤
                │ Booking                │
                │ Commission Rules       │
                │ Calculation Engine     │
                │ Contracts              │
                │ Ledger                 │
                │ Reconciliation         │
                │ Settlements            │
                │ Organizations          │
                └───────────┬────────────┘
                            │
             ┌──────────────┼──────────────┐
             │              │              │
             ▼              ▼              ▼
        PostgreSQL      NATS/Events     Workers
             │              │              │
             │              │              ▼
             │              │       Integrations
             │              │
             └──────────────┴──────────────┐
                                           │
                                           ▼
                              CONNECTOR PLATFORM
                                           │
                 ┌───────────┬─────────────┼──────────┐
                 ▼           ▼             ▼          ▼
               REST        SOAP           DB        Files

                 ▼           ▼             ▼          ▼
              Amadeus     Legacy ERP    Oracle      SFTP
              Sabre       PMS           MSSQL       CSV
              Hotelbeds   CRM           MySQL       Excel
              etc.                      MongoDB
```

El core nunca debería conocer directamente las particularidades de Amadeus, SAP, Oracle, Hotelbeds, Salesforce, etc.

El core solo debería conocer un **modelo canónico**.

---

# 2. Backend principal: Go

La recomendación principal para el backend es **Go**.

```text
Go
├── API
├── Rules Engine
├── Calculation Engine
├── Ledger
├── Reconciliation
├── Workers
└── Connector Runtime
```

## Motivos

- Buen rendimiento
- Concurrencia nativa
- Binarios simples
- Excelente para APIs
- Excelente para workers
- Buen comportamiento en contenedores
- Bajo consumo relativo
- Fácil distribución self-hosted
- Buen soporte para gRPC y Protobuf
- Fácil operación en Linux y Kubernetes

Node.js y Python pueden usarse en componentes auxiliares, especialmente IA, ETL o herramientas, pero no serían la primera elección para el core financiero.

---

# 3. Empezar como Modular Monolith

No se recomienda comenzar con decenas de microservicios.

En una primera fase:

```text
commission-platform
│
├── /booking
├── /rules
├── /calculation
├── /ledger
├── /contract
├── /reconciliation
├── /settlement
├── /integration
├── /organization
└── /audit
```

Todo dentro del mismo proyecto Go.

Los módulos deben tener límites internos claros para poder extraerse posteriormente.

Ejemplo:

```text
                    Core API
                       │
       ┌───────────────┼────────────────┐
       │               │                │
    Booking          Rules           Ledger
                       │
                       ▼
                  Calculation
```

Cuando crezca el volumen:

```text
                    Core API
                       │
                       ▼
                  Event Bus
                       │
             ┌─────────┴─────────┐
             ▼                   ▼
     Calculation Workers   Reconciliation
```

---

# 4. PostgreSQL como base de datos oficial

La base de datos interna recomendada es:

> **PostgreSQL**

No se recomienda soportar múltiples motores como almacenamiento principal del core.

Evitar:

```text
Commission Engine
      │
      ├── PostgreSQL
      ├── MySQL
      ├── Oracle
      ├── MSSQL
      ├── DB2
      └── MongoDB
```

Preferible:

```text
             CORE
               │
               ▼
          PostgreSQL
               │
       Canonical Storage
```

## Ventajas

- SQL robusto
- JSONB
- Índices avanzados
- Particionado
- Transacciones
- Buen ecosistema
- Muy adecuado para ledger y operaciones financieras
- Fácil self-hosting

Ejemplo:

```sql
booking
---------------------------
id
organization_id
external_id
supplier_id
amount
currency
booking_date
travel_date
status
metadata JSONB
```

`metadata` permite extender el modelo:

```json
{
  "hotel_chain": "Hilton",
  "rate_plan": "ABC123",
  "amadeus_office_id": "MAD123",
  "supplier_field_xyz": "..."
}
```

---

# 5. Dinero: nunca usar float

Para cálculos financieros no se debe usar floating point como valor definitivo.

Evitar:

```text
1099.99 * 0.13
```

usando float.

Usar:

- Decimal
- Fixed-point
- NUMERIC / DECIMAL en PostgreSQL
- Reglas de redondeo explícitas

Ejemplo:

```text
1099.99 EUR
×
0.13
=
142.9987

Rounding:
HALF_UP

Precision:
2

Resultado:
143.00 EUR
```

Cada regla podría incluir:

```json
{
  "rounding": "HALF_UP",
  "precision": 2
}
```

---

# 5 bis. Multi-divisa y tipos de cambio (FX)

El sector travel es cross-border por defecto. La multi-divisa debe contemplarse desde el primer día, aunque los primeros clientes operen en una sola divisa.

## Principio fundamental

> El importe original de la reserva nunca se convierte ni se muta.

La conversión siempre es un valor derivado, calculado y registrado. Nunca destructiva.

## Dinero como par (valor, divisa)

Toda cantidad monetaria del sistema viaja siempre acompañada de su divisa ISO 4217:

```json
{
  "amounts": {
    "gross": { "value": "2500.00", "currency": "USD" },
    "net":   { "value": "2100.00", "currency": "USD" }
  }
}
```

En PostgreSQL:

```text
value    NUMERIC
currency CHAR(3)
```

La precisión menor se toma de la tabla ISO 4217:

```text
EUR  2 decimales
JPY  0 decimales
BHD  3 decimales
```

## Tres divisas en juego

```text
transaction_currency   Divisa original de la reserva. Nunca se altera.
base_currency          Divisa de reporting de la organización.
contract_currency      Divisa en la que está redactado un contrato o regla.
```

Una regla declara en qué divisa expresa sus condiciones y resultados:

```json
{
  "name": "Hotelbeds Mexico",
  "currency_scope": {
    "evaluation_currency": "MXN",
    "on_mismatch": "convert"
  }
}
```

## FX Rate Provider abstraction

Igual que el resto de dependencias externas, el acceso a tipos de cambio se abstrae:

```go
type FXRateProvider interface {

    Rate(
        from string,
        to string,
        at time.Time,
    ) (Rate, error)

}
```

Implementaciones posibles:

```text
ecb              Banco Central Europeo (referencia diaria, gratuita)
manual           Tabla mantenida por el cliente
custom_endpoint  API privada del cliente
```

Compatible con el principio de cero dependencias cloud obligatorias.

## Versionado de tipos de cambio

Los rates son históricos e inmutables, igual que las reglas:

```sql
fx_rate
---------------------------
id
base_currency     CHAR(3)
quote_currency    CHAR(3)
rate              NUMERIC(18,8)
source            TEXT
valid_from        TIMESTAMPTZ
valid_to          TIMESTAMPTZ
created_at
created_by
```

Nunca se sobrescribe un rate. Se añade una nueva versión con vigencia.

## Fecha ancla de conversión

Cada organización configura qué fecha ancla la conversión:

```yaml
fx:

  provider: ecb

  anchor_date: booking_date
  # booking_date | travel_date | calculation_date

  max_rate_age_hours: 26
  # alerta si el rate aplicable es más antiguo
```

La política debe ser consistente para poder auditar y reproducir cálculos.

## Puntos de conversión explícitos

La conversión solo ocurre en puntos definidos:

```text
1. EVALUACIÓN DE REGLAS
   Si una condición compara contra otra divisa
   (ej. amount > 1000 EUR con reserva en USD),
   se convierte solo para comparar.

2. CÁLCULO
   Las comisiones porcentuales son neutras a la divisa.
   Los importes fijos (ej. fee €25) requieren conversión
   a la divisa de la regla antes de aplicarse.

3. LEDGER
   Cada entrada registra su divisa nativa y,
   opcionalmente, el equivalente en base_currency.

4. LIQUIDACIÓN
   La divisa de liquidación se elige al liquidar.
   Usa el rate de esa fecha, con su propio snapshot.
```

Regla de oro: cada conversión genera exactamente un redondeo y deja constancia. Nunca se reconvierte un importe ya convertido.

## Triangulación

Si no existe par directo (ej. USD → MXN), se triangula vía base_currency:

```text
rate(USD, EUR) × rate(EUR, MXN) = rate efectivo USD → MXN

Un único redondeo al final.
```

## Snapshot de FX en cada cálculo

Para mantener el motor determinista, todo CalculationResult registra el rate utilizado:

```json
{
  "fx": [
    {
      "pair": "USD/EUR",
      "rate": "0.92340000",
      "source": "ecb",
      "rate_date": "2026-08-20",
      "anchor": "booking_date"
    }
  ]
}
```

Sin snapshot, recalcular un histórico con rates de hoy rompería la reproducibilidad. Con snapshot:

```text
mismo booking
+
misma versión de reglas
+
mismo snapshot de FX
=
mismo resultado
```

## Redondeo

La conversión redondea una vez, al final, con modo explícito:

```json
{
  "rounding": "HALF_UP",
  "precision": 2
}
```

Por defecto, la precisión es la de las unidades menores de la divisa destino según ISO 4217.

## Ejemplo end-to-end

```text
Reserva:        2.500 USD
Regla:          17% expresada en EUR
Base org:       EUR
Anchor:         booking_date

Rate USD/EUR:   0.9234  (ECB, fecha de reserva)

Comparación:    2.500 × 0.9234 = 2.308,50 EUR > 1.000 ✓

Comisión:       17% × 2.308,50 EUR = 392,44 EUR  (HALF_UP)

Ledger:         +392,44 EUR  (referencia nativa: 2.500 USD)

Liquidación posterior en MXN:
nueva conversión EUR → MXN con rate de fecha de liquidación,
nuevo snapshot, sin reutilizar el rate del cálculo.
```

## Impacto en el resto de la arquitectura

```text
Modelo canónico (§14)   amounts como pares { value, currency }
Ledger (§22)            saldos siempre por divisa; nunca mezclados
Versionado (§23)        fx_rate versionado igual que las reglas
Interfaces (§32)        FXRateProvider junto a EventBus, etc.
Alcance v1              Money {valor, divisa} desde el día uno;
                        FX versionado en v2 (ver alcance_v1)
```

Añadir multi-divisa sobre datos históricos es uno de los refactorings más caros que existen. El esquema debe nacer preparado.

---

# 6. Rule Engine

La recomendación es usar:

> **Custom Rule DSL + CEL**

No almacenar código arbitrario de usuarios.

Ejemplo de regla:

```json
{
  "name": "Hotelbeds Mexico",
  "version": 17,
  "when": {
    "expression": "supplier == 'hotelbeds' && country == 'MX' && amount > 1000"
  },
  "then": {
    "commission": {
      "type": "percentage",
      "value": "0.17",
      "base": "booking.net_amount"
    }
  }
}
```

## CEL

CEL puede utilizarse para expresiones como:

```text
country == "MX"

supplier == "hotelbeds"

amount > 5000

agent.level == "gold"

supplier == "hotelbeds" &&
country == "MX" &&
amount > 5000
```

Permite reglas expresivas sin ejecutar código arbitrario.

Nota: CEL no puede expresar condiciones sobre datos históricos (volumen mensual, tiers, rappels retroactivos). Esos casos se resuelven con agregados inyectados como contexto y outcomes tipados. Ver documento complementario:

> `travel_commission_engine_reglas_agregacion.md`

La resolución de jerarquía entre reglas candidatas (precedencia, desempates, modos de composición y validación pre-deploy) está definida en:

> `../mvp/travel_commission_engine_jerarquia_reglas.md`

---

# 7. Calculation Engine

El motor de cálculo debe ser determinista y, en la medida de lo posible, una función pura.

Conceptualmente:

```text
Calculate(
    Booking,
    Rules,
    Context
) -> CalculationResult
```

Entrada:

```json
{
  "booking": {},
  "rules_version": "2026.08.1",
  "context": {}
}
```

Salida:

```json
{
  "gross_amount": "2500.00",
  "commission": "425.00",
  "allocations": [
    {
      "beneficiary": "agency",
      "amount": "297.50"
    },
    {
      "beneficiary": "agent",
      "amount": "127.50"
    }
  ],
  "rules_applied": [
    "HOTELBEDS_BASE",
    "MEXICO_OVERRIDE"
  ]
}
```

El mismo input con la misma versión de reglas debe producir siempre el mismo output.

Esto es esencial para:

- Auditoría
- Reprocesamiento
- Debugging
- Reconciliación
- Compliance
- Reproducibilidad histórica

---

# 8. API-first

Externamente:

> **REST + JSON + OpenAPI**

Ejemplos:

```text
POST /v1/bookings

POST /v1/calculations

GET /v1/calculations/{id}

POST /v1/rules

GET /v1/commissions

POST /v1/reconciliations
```

El contrato OpenAPI debería considerarse parte central del producto.

A partir de OpenAPI se pueden generar SDKs:

```text
TypeScript SDK
Python SDK
Java SDK
C# SDK
Go SDK
PHP SDK
```

---

# 9. Comunicación interna: gRPC + Protobuf

Para comunicación interna entre componentes:

> **gRPC + Protocol Buffers**

Ejemplos:

```text
API
 │
 │ gRPC
 ▼
Calculation Workers
```

o:

```text
Connector
   │
   │ gRPC
   ▼
Platform
```

Ventajas:

- Contratos tipados
- Buen rendimiento
- Generación de código
- Multilenguaje
- Ideal para Connector SDK

---

# 10. Connector SDK

Una pieza estratégica del producto debe ser un protocolo estándar de conectores.

Nombre conceptual:

> **Commission Connector Protocol**

Ejemplo:

```protobuf
service BookingConnector {

    rpc TestConnection(...)
        returns (...);

    rpc FetchBookings(...)
        returns (...);

    rpc FetchBooking(...)
        returns (...);

    rpc FetchSuppliers(...)
        returns (...);

    rpc Acknowledge(...)
        returns (...);
}
```

Esto permite crear:

```text
connector-amadeus
connector-sabre
connector-hotelbeds
connector-salesforce
connector-sap
connector-oracle
connector-mssql
connector-custom-erp
```

Los conectores no tienen que estar escritos en Go.

```text
CORE
Go
 │
 │ gRPC
 ▼
Connector SDK
 │
 ├── Java
 ├── .NET
 ├── Go
 ├── Python
 └── Node
```

Un cliente con un ERP legacy Java puede escribir su propio conector Java.

---

# 11. Adaptación a cualquier base de datos

La compatibilidad con bases de datos externas debe resolverse mediante conectores o CDC.

```text
                    Commission Platform
                            ▲
                            │
                       Canonical Events
                            │
                ┌───────────┴───────────┐
                │                       │
             Connector                 CDC
                │                       │
       ┌────────┼────────┐              │
       │        │        │              │
     Oracle   MSSQL    MySQL         Debezium
```

## CDC

Debezium puede utilizarse para capturar cambios en:

- PostgreSQL
- MySQL
- MariaDB
- SQL Server
- Oracle
- DB2
- MongoDB
- otras plataformas soportadas

Ejemplo:

```text
SQL Server
    │
    │ CDC
    ▼
Debezium
    │
    ▼
booking.created
booking.updated
booking.cancelled
    │
    ▼
Commission Engine
```

Esto permite integrarse con sistemas existentes sin obligar al cliente a modificar su aplicación.

---

# 12. Event Bus

La recomendación inicial es:

> **NATS JetStream**

Ejemplo:

```text
Booking created
       │
       ▼
     NATS
       │
 ┌─────┼────────┬──────────┐
 ▼     ▼        ▼          ▼
Calc  Audit  Analytics  Reconcile
```

Eventos posibles:

```text
booking.created
booking.updated
booking.cancelled

commission.calculated
commission.updated

settlement.created
settlement.paid

reconciliation.failed

contract.created
rule.updated
```

Nota: escribir el estado en PostgreSQL y publicar a NATS son sistemas distintos. Sin transaccional outbox, orden por agregado e idempotencia en consumidores, un sistema financiero perderá o duplicará eventos. Ver documento complementario:

> `travel_commission_engine_eventos_consistencia.md`

---

# 13. Soporte Kafka para enterprise

No se recomienda usar Kafka como dependencia obligatoria desde la v1.

Crear una interfaz:

```text
EventBus
```

Implementaciones:

```text
NATS
Kafka
InMemory
```

Cliente estándar:

```yaml
events:
  provider: nats
```

Cliente enterprise:

```yaml
events:
  provider: kafka

  brokers:
    - kafka01.internal:9092
    - kafka02.internal:9092
```

---

# 14. Modelo canónico de datos

Todos los sistemas externos deben transformarse a un modelo interno común.

Ejemplo de fuentes:

Amadeus:

```json
{
  "PNR": "...",
  "TOTAL": "...",
  "OFFICE_ID": "..."
}
```

SAP:

```text
BOOK_ID
NET_AMT
CCY
```

Internamente:

```json
{
  "booking_id": "...",
  "external_id": "...",
  "supplier": {},
  "product": {
    "type": "hotel"
  },
  "amounts": {
    "gross": { "value": "2500.00", "currency": "EUR" },
    "net":   { "value": "2100.00", "currency": "EUR" }
  },
  "customer": {},
  "travel": {
    "start": "...",
    "end": "..."
  },
  "metadata": {}
}
```

El modelo canónico es una de las piezas más importantes de toda la arquitectura.

---

# 15. Mapping Engine

Crear un motor de mapeo permite convertir integraciones nuevas en configuración.

Ejemplo:

```text
SAP.NET_AMT
        ↓
booking.amount.net

SAP.CURRENCY_CODE
        ↓
booking.currency

SAP.AGENT_ID
        ↓
booking.agent.external_id
```

Configuración:

```yaml
mapping:

  booking.external_id:
    source: BOOK_ID

  booking.amount.net:
    source: NET_AMT

  booking.currency:
    source: CCY
```

---

# 16. Webhooks

El sistema debe emitir webhooks.

Ejemplo:

```text
POST customer-system.com/hooks/commission
```

Eventos:

```text
commission.calculated
commission.confirmed
commission.disputed
settlement.ready
payment.received
```

Campos importantes:

```text
event_id
idempotency_key
timestamp
version
tenant
schema_version
```

---

# 17. Self-hosted simple

Para clientes medianos:

> **Docker Compose**

Servicios:

```text
commission-api
commission-worker
commission-ui
postgres
nats
```

Instalación:

```bash
docker compose up -d
```

Ideal para:

- Agencias medianas
- PoCs
- Private cloud
- Servidores dedicados
- Instalaciones simples

---

# 18. Self-hosted enterprise

Para enterprise:

> **Kubernetes + Helm**

Instalación:

```bash
helm install commission-engine ./chart
```

Ejemplo de configuración:

```yaml
database:

  mode: external

  host: postgres.customer.internal

events:

  provider: kafka

auth:

  provider: oidc

storage:

  provider: s3

ai:

  enabled: false
```

---

# 19. Ninguna dependencia cloud obligatoria

El core debe funcionar sin depender de:

```text
AWS
Azure
GCP
OpenAI
SaaS externo
```

Esto es especialmente importante para:

- Grandes grupos de viajes
- Bancos
- Airlines
- TMCs
- Gobierno
- Empresas con redes restringidas
- Clientes con requisitos regulatorios

---

# 20. Autenticación enterprise

No conviene construir un sistema de identidad completo desde cero.

Soportar:

```text
OIDC
SAML
```

Proveedores posibles:

```text
Microsoft Entra ID
Okta
Keycloak
Otros IdP compatibles
```

Keycloak puede ofrecerse como opción self-hosted.

Nota: OIDC/SAML resuelve autenticación, no autorización. El modelo de permisos, roles y aprobaciones (maker-checker) es parte del core. Ver documento complementario:

> `travel_commission_engine_autorizacion_aprobaciones.md`

---

# 21. Multi-tenant desde el principio

Todas las entidades importantes deben incluir:

```text
organization_id
```

Ejemplos:

```text
Booking
Commission
Rule
Contract
Supplier
Agent
Settlement
```

Esto permite usar el mismo código para:

```text
SaaS multi-tenant
```

y:

```text
Enterprise single-tenant self-hosted
```

---

# 22. Ledger inmutable

No modificar directamente valores financieros históricos.

Evitar:

```text
commission.amount = newValue
```

Preferir:

> **Append-only Ledger**

Ejemplo:

```text
CALCULATED
+€425

ADJUSTMENT
-€25

BONUS
+€50

CLAWBACK
-€100
```

Resultado:

```text
€350
```

Ventajas:

- Auditoría
- Historial
- Reconciliación
- Trazabilidad
- Compliance
- Reversibilidad

Nota: append-only como historial plano no basta para settlements y payments. Se requiere doble partida formal (débito/crédito con invariante de cuadre, cuentas por divisa y por contraparte). Ver documento complementario:

> `travel_commission_engine_ledger_doble_partida.md`

El proceso que mueve las comisiones por su ciclo de vida hasta el pago (ESTIMATED → PAID, batches, disputas, clawbacks y la interfaz PaymentProvider) está definido en:

> `travel_commission_engine_liquidacion_settlements.md`

---

# 23. Versionar todo

Ejemplo:

```text
CommissionRule

id
version
valid_from
valid_to
created_at
created_by
status
```

Una reserva histórica puede utilizar:

```text
Rule version 17
```

aunque hoy exista:

```text
Rule version 24
```

Esto permite reproducir exactamente cálculos históricos.

---

# 24. Workflows largos

En la v1, tabla de jobs y workers simples:

```text
Go Workers
+
PostgreSQL
+
NATS
```

Más adelante se puede incorporar:

> **Temporal**

Especialmente para workflows como:

```text
Booking
 ↓
Wait until checkout
 ↓
Confirm commission
 ↓
Wait supplier statement
 ↓
Reconcile
 ↓
Generate settlement
 ↓
Wait payment
 ↓
Close
```

No es necesario introducir Temporal desde el primer día.

---

# 25. Observabilidad

Incorporar desde el principio:

> **OpenTelemetry**

Ejemplo de trace:

```text
API request
      │
      ▼
booking.created
      │
      ▼
calculation
      │
      ▼
rule evaluation
      │
      ▼
ledger
      │
      ▼
webhook
```

El cliente puede elegir el backend:

```text
Grafana
Prometheus
Jaeger
Datadog
Elastic
etc.
```

---

# 26. IA separada del core financiero

La IA nunca debe formar parte del cálculo financiero determinista.

Evitar:

```text
Commission Calculation
        │
        ▼
       LLM
        │
        ▼
Commission
```

Preferible:

```text
                CORE ENGINE

Rules ───────► Calculation ─────► Ledger
                   ▲
                   │
             deterministic


                 AI LAYER

Contract extraction
Rule suggestions
Anomaly detection
Natural language queries
Optimization
```

Ejemplo:

```text
PDF contrato
     │
     ▼
    AI
     │
     ▼
proposed_rule.json
     │
     ▼
validation
     │
     ▼
human approval
     │
     ▼
Rule Engine
```

---

# 27. AI Provider Abstraction

No acoplar la plataforma a un único proveedor de IA.

Conceptualmente:

```go
type AIProvider interface {

    ExtractContract(...)

    SuggestRules(...)

    ExplainAnomaly(...)

}
```

Posibles implementaciones:

```text
Cloud LLM
Customer LLM
Local model
Private AI endpoint
```

Configuración:

```yaml
ai:

  provider: custom

  endpoint:
    https://llm.internal.customer
```

O:

```yaml
ai:

  enabled: false
```

---

# 28. Stack recomendado

| Capa | Tecnología |
|---|---|
| Core backend | **Go** |
| Public API | **REST + JSON** |
| API contract | **OpenAPI** |
| Internal RPC | **gRPC + Protobuf** |
| Rules | **Custom DSL + CEL** |
| Aggregation rules | **Aggregates declarativos + outcomes tipados (ver doc de reglas por agregación)** |
| Rule resolution | **Jerarquía lexicográfica + FIRST_MATCH/STACK (ver doc de jerarquía de reglas)** |
| Main database | **PostgreSQL** |
| Flexible fields | **JSONB** |
| Financial values | **Decimal / fixed point** |
| Ledger | **Append-only + doble partida (ver doc de ledger)** |
| Multi-divisa | **Money (valor + divisa ISO 4217) + FX versionado** |
| Impuestos | **Módulo fiscal versionado + snapshots (ver doc de impuestos)** |
| Events | **NATS JetStream** |
| Event consistency | **Transactional outbox + orden por agregado (ver doc de eventos y consistencia)** |
| Liquidación | **Ciclo de vida + batches + PaymentProvider (ver doc de liquidación)** |
| Enterprise events | Kafka adapter |
| DB ingestion / CDC | **Debezium** |
| Cache | Redis opcional |
| Workflows | Go workers → Temporal posteriormente |
| Auth | OIDC / SAML |
| Autorización | **RBAC + maker-checker (ver doc de autorización y aprobaciones)** |
| Datos personales | **Minimización + crypto-shredding (ver doc de protección de datos)** |
| Self-host small | Docker Compose |
| Self-host enterprise | Kubernetes + Helm |
| Observability | **OpenTelemetry** |
| AI | Servicio separado / provider abstraction |
| Frontend | React + TypeScript |
| Object/files | S3-compatible interface |
| Configuration | YAML + env + secrets |

---

# 29. Arquitectura de repositorio

Se recomienda comenzar con monorepo:

```text
commission-platform/

├── api/
│   └── openapi.yaml
│
├── proto/
│
├── cmd/
│   ├── api/
│   ├── worker/
│   └── connector-runtime/
│
├── internal/
│
│   ├── booking/
│   ├── commission/
│   ├── rules/
│   ├── calculation/
│   ├── ledger/
│   ├── settlement/
│   ├── reconciliation/
│   ├── contract/
│   └── organization/
│
├── pkg/
│   ├── money/
│   ├── events/
│   ├── connector/
│   └── ruleengine/
│
├── connectors/
│   ├── generic-rest/
│   ├── generic-db/
│   └── generic-file/
│
├── web/
│
├── deployments/
│   ├── docker/
│   └── helm/
│
└── sdk/
    ├── typescript/
    ├── python/
    ├── java/
    ├── dotnet/
    └── go/
```

---

# 30. Arquitectura estratégica

La plataforma puede entenderse como tres grandes capas.

```text
                    TRAVEL FINANCIAL ENGINE

        ┌─────────────────────────────────────┐
        │                                     │
        │         COMMISSION CORE             │
        │                                     │
        │ Rules                               │
        │ Calculations                        │
        │ Ledger                              │
        │ Settlements                         │
        │ Reconciliation                      │
        │                                     │
        └─────────────────┬───────────────────┘
                          │
            ┌─────────────┴──────────────┐
            │                            │
            ▼                            ▼

   INTEGRATION PLATFORM               AI LAYER

   Connectors                         Contracts → Rules
   CDC                                Anomalies
   API                                Forecasting
   Webhooks                           Optimization
   Mapping                            Copilot
```

Esto es más potente que construir únicamente una API de comisiones.

---

# 31. Alcance v1

La definición completa de la primera versión construible — qué se construye, qué queda schema-ready y qué se difiere — vive en el documento dedicado:

> `../mvp/travel_commission_engine_alcance_v1.md`

En esencia, la v1 es un motor embebible:

```text
Go + PostgreSQL + OpenAPI REST + CEL

React (dashboard + editor de reglas)

Docker Compose
```

Sin bus de eventos, sin doble partida, sin KMS: tabla de jobs,
movimientos append-only y cero PII. El resto de este documento
describe la arquitectura objetivo por fases.

Las tres integraciones genéricas pasan a fases posteriores;
la v1 resuelve la ingesta con API, CSV y carga manual.

---

# 32. Interfaces importantes desde el inicio

Aunque solo exista una implementación inicial, diseñar interfaces para:

```text
DatabaseAdapter
EventBus
ObjectStorage
IdentityProvider
AIProvider
FXRateProvider
PaymentProvider
Connector
```

Esto permite posteriormente adaptar la solución a clientes enterprise sin rehacer el core.

---

# 33. Estrategia de despliegue

El mismo código debe poder desplegarse como:

```text
SaaS multi-tenant
```

o:

```text
Docker en un servidor del cliente
```

o:

```text
Kubernetes + PostgreSQL + Kafka
dentro de una multinacional
```

sin mantener tres productos diferentes.

---

# 34. Componentes tecnológicos más importantes

Las piezas que más conviene diseñar correctamente desde el principio son:

## 1. Canonical Data Model

Define cómo se representan internamente:

- Bookings
- Suppliers
- Agents
- Contracts
- Products
- Commissions
- Settlements
- Payments

## 2. Rule DSL

Define cómo negocio puede configurar comisiones sin escribir código.

## 3. Calculation Engine

Debe ser:

- Determinista
- Rápido
- Versionado
- Auditable
- Reproducible

## 4. Connector Protocol

Define cómo se conecta la plataforma con cualquier sistema externo.

---

# 35. Resumen

La arquitectura recomendada puede resumirse así:

```text
                    CLIENT SYSTEMS

      REST / SOAP / DB / CDC / CSV / SFTP / GDS
                         │
                         ▼
                CONNECTOR PLATFORM
                         │
                         ▼
                 CANONICAL MODEL
                         │
                         ▼
              ┌────────────────────┐
              │ COMMISSION CORE    │
              │                    │
              │ Rules              │
              │ Calculation        │
              │ Ledger             │
              │ Reconciliation     │
              │ Settlement         │
              └─────────┬──────────┘
                        │
             ┌──────────┼───────────┐
             ▼          ▼           ▼
         PostgreSQL    NATS      Webhooks
                                    │
                                    ▼
                              Client Systems

                  ┌──────────────────┐
                  │     AI LAYER     │
                  │                  │
                  │ Contract parsing │
                  │ Rule suggestions │
                  │ Anomalies        │
                  │ Forecasting      │
                  │ Optimization     │
                  └──────────────────┘
```

La recomendación general es mantener el core pequeño, determinista y desacoplado, y concentrar la flexibilidad en:

> **Canonical Data Model + Rule DSL + Calculation Engine + Connector Protocol**

Esas cuatro piezas pueden convertirse en el verdadero núcleo tecnológico y defensible de la plataforma.
