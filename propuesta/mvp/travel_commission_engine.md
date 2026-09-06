# Travel Commission Engine

> **Estado documental:** visión de producto y arquitectura objetivo; no es la fuente normativa de alcance ni de avance. Cuando una fase o capacidad difiera, prevalecen [Alcance v1](./travel_commission_engine_alcance_v1.md) y el [roadmap](./roadmap.md). Al 2026-09-04 solo existe una implementación parcial de la F1.

## Infraestructura financiera para gestionar comisiones, márgenes e incentivos en empresas de viajes

## 1. Concepto

La idea puede convertirse en un producto B2B potente si el núcleo no se plantea simplemente como un “calculador de comisiones”, sino como un **motor configurable de reglas económicas para el sector travel**.

Nombre provisional:

> **Travel Commission Engine**

Una plataforma que centraliza la configuración, cálculo, liquidación, auditoría y optimización de todas las comisiones asociadas a la venta de productos turísticos:

- Hoteles
- Vuelos
- Paquetes
- Actividades
- Cruceros
- Seguros
- Transfers
- Rent-a-car
- Reservas en general

El problema que resuelve es que una agencia puede tener cientos o miles de combinaciones diferentes:

- Un hotel paga 12% de comisión.
- Otro paga 15%, pero solo en determinadas fechas.
- Booking, Expedia u otro proveedor aplica otra estructura.
- Un agente comercial se lleva un porcentaje de la comisión de la agencia.
- Un afiliado tiene otra comisión.
- Una franquicia se queda con un fee.
- Hay overrides por país, destino, producto, proveedor, campaña o cliente.
- Puede haber bonos por volumen.
- Puede haber clawbacks si se cancela la reserva.
- Hay comisiones diferentes dependiendo de si la tarifa es neta, comisionable o merchant.
- Pueden existir impuestos, fees, rappels y acuerdos comerciales adicionales.

El motor debería responder una pregunta sencilla:

> **Para esta reserva concreta, ¿quién cobra cuánto, por qué y según qué regla?**

Ejemplo:

```text
Reserva: €2.000

Precio venta:              €2.000
Coste proveedor:           €1.650
Margen bruto:                €350

Comisión hotel 15%:          €300
Bonus producción:             €40
Fee plataforma:              -€20

Comisión total agencia:      €320

Distribución:
Agencia matriz               €160
Agente comercial              €96
Franquicia                    €32
Afiliado                      €32
```

La clave es que cada importe sea **trazable hasta la regla que lo generó**.

---

## 2. Motor de configuración de comisiones

El corazón del producto sería un sistema **no-code / low-code** que permita al negocio configurar reglas sin depender del equipo de desarrollo.

Una regla podría tener esta estructura:

### Condiciones

```text
SI

Proveedor = Hotelbeds
Y destino = México
Y tipo_producto = Hotel
Y fecha_checkin entre 01/06 y 31/08
Y importe_reserva > €1.000
```

### Resultado

```text
ENTONCES

Comisión agencia = 14%
Comisión agente = 30% de la comisión de agencia
Bonus = 2% adicional si volumen mensual > €50.000
```

### Tipos de reglas

| Tipo | Ejemplo |
|---|---|
| Comisión porcentual | 15% del valor de reserva |
| Comisión fija | €25 por reserva |
| Comisión escalonada | 10% hasta 100k, 12% a partir de 100k |
| Comisión por margen | 20% del margen generado |
| Override | Agente senior recibe +2% |
| Revenue share | 70/30 |
| Bonus | +€500 al llegar a 50 reservas |
| Rappel | +1% retroactivo por volumen |
| Fee | €5 por transacción |
| Penalización | -100% comisión en cancelaciones |
| Split | Distribución entre varios participantes |

---

## 3. Jerarquía de reglas

Las reglas podrían configurarse en distintos niveles:

```text
Global
 ↓
Mercado
 ↓
País
 ↓
Canal
 ↓
Agencia
 ↓
Sucursal
 ↓
Agente
 ↓
Proveedor
 ↓
Producto
 ↓
Contrato
 ↓
Reserva
```

Ejemplo:

```text
Hoteles → 12%
Hotelbeds → 14%
Hotelbeds + México → 15%
Hotelbeds + México + Cancún → 17%
Hotel X en Cancún → 20%
```

El motor debe determinar automáticamente cuál es la regla más específica aplicable.

Esto convierte el producto en algo mucho más sofisticado que un simple sistema de porcentajes.

---

## 4. Motor de cálculo

El segundo gran componente sería el **Calculation Engine**.

Cada vez que entra una reserva:

```text
BOOKING CREATED
```

el motor recibe datos como:

```json
{
  "agency": "Travel Agency X",
  "agent": "Carlos",
  "provider": "Hotelbeds",
  "product": "hotel",
  "destination": "Cancun",
  "hotel": "Hotel ABC",
  "booking_value": 2500,
  "currency": "EUR",
  "check_in": "2026-09-10",
  "check_out": "2026-09-15"
}
```

Y devuelve:

```json
{
  "agency_commission": 425,
  "agent_commission": 127.5,
  "platform_fee": 20,
  "net_agency_revenue": 277.5
}
```

Además, debería incluir trazabilidad:

```text
Rule applied:
HOTELBEDS_MEXICO_SUMMER_17

Reason:
Provider = Hotelbeds
Country = Mexico
Travel date within campaign
```

Esto es fundamental para auditoría y reclamaciones.

---

## 5. Lifecycle de la comisión

Una comisión no debería ser simplemente “calculada”.

Debería tener un ciclo de vida:

```text
ESTIMATED
↓
CONFIRMED
↓
ACCRUED
↓
PAYABLE
↓
PAID
```

También pueden existir estados alternativos:

```text
CANCELLED
DISPUTED
ADJUSTED
CLAWBACK
```

Ejemplo:

Un cliente reserva hoy un hotel por €3.000.

El sistema calcula:

```text
Comisión estimada: €450
```

Pero esa comisión no se considera ganada hasta que el cliente realiza el checkout.

```text
checkout
    ↓
commission earned
    ↓
provider invoice
    ↓
payment received
    ↓
commission settled
```

Esto abre la puerta a construir también la parte financiera.

---

## 6. Reconciliación

Una de las funcionalidades con mayor valor económico puede ser la conciliación automática.

Ejemplo:

```text
La agencia cree que debería cobrar: €143.520
El proveedor informa:              €138.900
```

El sistema detecta automáticamente las diferencias.

```text
Reserva #93212

Esperado: €340
Proveedor: €280
Diferencia: €60
```

Posible causa:

```text
Commission expected: 17%
Commission reported: 14%
```

El sistema genera una incidencia.

Esto permite detectar **revenue leakage**, es decir, dinero que la agencia debería cobrar pero no está cobrando.

---

## 7. Dashboard financiero

El producto debería incluir un dashboard orientado a negocio.

### Comisión generada

```text
Este mes

Ventas:             €4.2M
Comisiones:         €487k
Margen:             €312k
Comisiones agentes: €95k
```

### Por proveedor

```text
Hotelbeds        €142k
Booking.com       €98k
Expedia           €76k
Amadeus           €45k
Otros            €126k
```

### Por agente

```text
Carlos   €21.450
Laura    €18.230
Miguel   €16.920
```

### Por destino

```text
México
España
República Dominicana
Estados Unidos
Italia
```

---

## 8. Simulador de comisiones

Una funcionalidad especialmente atractiva sería un simulador de escenarios.

Ejemplo:

> ¿Qué habría pasado si la comisión de Hotelbeds hubiera sido del 15% en vez del 13%?

Resultado:

```text
Reservas afectadas:        12.421

Comisión actual:          €812.340
Nueva comisión:           €937.315

Incremento:               €124.975
```

Otro ejemplo:

> Si doy a mis agentes un 35% de la comisión en vez del 30%, ¿cuánto me cuesta al año?

Respuesta:

```text
Coste adicional estimado:

€426.000 / año
```

Esto convierte el motor en una herramienta de **planificación comercial**.

---

## 9. Marketplace y contratos de proveedores

Podría existir un módulo específico para guardar contratos comerciales.

Ejemplo:

### Hotelbeds

```text
Base commission: 14%

Mexico:
+2%

Volume > €1M / year:
+1%

Volume > €3M:
+2%

Luxury hotels:
+3%
```

El contrato podría traducirse automáticamente a reglas del motor.

Aquí aparece una de las primeras aplicaciones fuertes de IA.

---

## 10. IA: Contract-to-Rules

El usuario sube un contrato comercial en PDF:

```text
Contrato comercial Hilton 2026.pdf
```

La IA analiza el documento y detecta:

```text
Comisión base: 12%

España: 14%

Luxury properties: +2%

Producción > €500k: +1%

Cancelaciones:
No commission

Fecha vigencia:
01/01/2026 - 31/12/2026
```

Después propone automáticamente:

```text
Crear regla 1
Crear regla 2
Crear regla 3
Crear regla 4
```

El usuario solo tendría que revisar y aprobar.

Esta funcionalidad puede ser especialmente valiosa porque muchos acuerdos comerciales terminan viviendo en:

```text
PDFs
Excel
emails
ERP
CRM
memoria de empleados
```

---

## 11. Copilot de comisiones

La IA también puede funcionar como interfaz conversacional para consultar el sistema.

Ejemplos:

> ¿Cuánto ganamos con Marriott durante julio?

> ¿Qué proveedores nos dan menor margen en España?

> ¿Cuánto tenemos pendiente de cobrar de reservas ya consumidas?

> ¿Qué agentes tienen una comisión superior al promedio?

Ejemplo de respuesta:

```text
Tienes €184.230 pendientes de cobrar.

Los principales proveedores son:

Hotelbeds     €54.200
Expedia       €38.100
Marriott      €31.450
Otros         €60.480
```

---

## 12. IA para detectar anomalías

El sistema puede aprender cuáles son los patrones normales y detectar posibles errores.

Ejemplos:

```text
Esta reserva tiene una comisión anormalmente baja.
```

```text
Hotel ABC normalmente paga 17%.

Las últimas 23 reservas están calculándose al 12%.
```

```text
El margen de las reservas de Cancún cayó un 18% esta semana.
```

Esto puede detectar errores de configuración antes de que generen pérdidas significativas.

---

## 13. IA para optimizar acuerdos comerciales

Con suficiente información histórica, el producto puede evolucionar desde un sistema administrativo hacia un sistema de **Revenue Intelligence**.

Ejemplo:

> Expedia genera €4,2M de volumen anual, pero su comisión efectiva es 11,8%. Proveedores comparables generan 14,1%. Si se renegociara al 13,5%, el impacto estimado sería +€71.400/año.

Otro ejemplo:

> Si concentras un 8% más de reservas en Hotelbeds, alcanzarías el siguiente tier de rappel y generarías aproximadamente €94.000 adicionales.

Esto entra directamente en optimización de beneficios.

---

## 14. Arquitectura conceptual

La plataforma podría dividirse en varios motores.

```text
                    TRAVEL COMMISSION PLATFORM

                           Booking
                              │
                              ▼
                    ┌─────────────────┐
                    │ Booking Engine  │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │  Rules Engine   │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │Calculation Engine│
                    └────────┬────────┘
                             │
          ┌──────────────────┼──────────────────┐
          ▼                  ▼                  ▼

     Commissions          Margins            Fees
          │                  │                  │
          └──────────────────┼──────────────────┘
                             ▼

                   ┌──────────────────┐
                   │ Settlement Engine │
                   └────────┬─────────┘
                            │
                            ▼

                    Reconciliation
                            │
                            ▼

                      Payments
```

Sobre esta arquitectura existiría una capa:

```text
AI LAYER
```

Responsable de:

```text
Contract extraction
Anomaly detection
Forecasting
Rule suggestions
Revenue optimization
Natural language analytics
```

---

## 15. Integraciones

El producto ganaría mucho valor si fuera **API-first**.

Podría conectarse con:

```text
GDS
CRS
PMS
ERP
CRM
Booking engines
OTA
Wholesalers
Bedbanks
Payment providers
Accounting systems
```

Arquitectura de integración:

```text
Amadeus
Sabre
Travelport
Hotelbeds
Expedia
Booking
PMS
ERP
CRM
   │
   ▼
Travel Commission API
   │
   ▼
Rules Engine
```

De esta forma, el producto no necesita sustituir el sistema de reservas existente.

Se convierte en una **capa financiera encima de ellos**.

---

## 16. Modelo de datos básico

Entidades principales:

```text
Organization
Agency
Branch
Agent
Supplier
Product
Booking
Customer
Contract
CommissionPlan
CommissionRule
CommissionCalculation
CommissionAllocation
Settlement
Invoice
Payment
Reconciliation
Dispute
```

Una de las entidades más importantes sería:

```text
CommissionCalculation
```

Debería guardar siempre:

```text
input
rule
rule version
calculation
output
timestamp
```

Nunca se debería recalcular una comisión histórica utilizando reglas nuevas accidentalmente.

Por eso sería fundamental tener **versionado de reglas**.

---

## 17. Explainability

Cada cálculo debería permitir pulsar:

> **¿Por qué?**

Ejemplo:

```text
Comisión: €425
```

Y mostrar:

```text
Base reservation amount
€2.500

Supplier base commission
14%
= €350

Mexico campaign override
+2%
= €50

Luxury hotel bonus
+1%
= €25

TOTAL
€425
```

Esto reduce significativamente los conflictos entre:

```text
agencia
agente
contabilidad
proveedor
franquicia
```

---

## 18. Alcance inicial

La definición completa de la primera versión construible vive en el documento dedicado:

> `travel_commission_engine_alcance_v1.md`

En esencia, la v1 conserva cinco bloques de producto, ahora product-céntricos, y añade la conciliación básica necesaria para comparar lo esperado con lo cobrado:

```text
1. Catálogo de productos (hoteles, autos, paquetes...)
2. Reglas reutilizables: crear una vez, aplicar a N productos
3. El agente ve cuánto ganará ANTES de vender
4. Registro de la venta y cálculo confirmado
5. Saldos por agente y dashboard básico
6. Conciliación básica por CSV
```

Las reservas ocurren en los sistemas del cliente. El resto de esta propuesta es arquitectura objetivo por fases y no amplía por sí solo el alcance normativo.

---

## 19. Después del MVP base

La clasificación vigente está en [Alcance v1](./travel_commission_engine_alcance_v1.md#6-fuera-de-alcance-v1-con-destino): contratos de proveedor, tiered básico y conciliación CSV ya pertenecen a v1. La evolución posterior se separa así:

- **v1 Enterprise:** RBAC y approvals maker-checker.
- **v1.5:** conciliación rica por API y gestión de disputes/excepciones.
- **v2+:** `STACK`, splits a N partes, agregación persistente, rappels retroactivos, FX automático, settlements/payouts e invoices.

---

## 20. Tercera fase: IA

Una posible secuencia de evolución sería:

```text
1. Contract → Rules

2. AI commission assistant

3. Anomaly detection

4. Commission forecasting

5. Revenue leakage detection

6. Supplier negotiation intelligence

7. Automatic rule recommendations
```

---

## 21. Posicionamiento

Sería recomendable evitar venderlo como:

> Software para calcular comisiones.

Ese posicionamiento puede resultar demasiado limitado.

Una alternativa:

> **Revenue & Commission Infrastructure for Travel Companies.**

En español:

> **La infraestructura financiera para gestionar comisiones, márgenes e incentivos en empresas de viajes.**

El mercado potencial no estaría limitado a agencias tradicionales.

Podrían utilizarlo:

```text
Agencias de viaje
OTAs
TMCs
Tour operadores
Bedbanks
Networks de agencias
Franquicias
Marketplaces turísticos
Consolidadores
Hoteles
Empresas de actividades
Plataformas B2B travel
```

Especialmente interesante sería venderlo a organizaciones que gestionan **cientos de agencias o miles de agentes**, donde la complejidad de las comisiones crece de forma considerable.

---

## 22. Evolución estratégica del producto

La evolución natural puede plantearse así:

```text
Commission Calculator
        ↓
Commission Management
        ↓
Revenue Reconciliation
        ↓
Revenue Intelligence
        ↓
AI Revenue Optimization
```

El **moat** no estaría simplemente en saber calcular un porcentaje.

Estaría en disponer de un motor que entienda:

- Contratos
- Jerarquías
- Reservas
- Proveedores
- Excepciones
- Incentivos
- Conciliaciones
- Márgenes
- Comisiones
- Histórico de reglas

Y que, utilizando todos esos datos, sea capaz de decirle a una empresa de viajes:

> **Dónde está perdiendo dinero y cómo puede ganar más.**

En ese punto, el producto deja de ser un módulo administrativo y puede convertirse en una **plataforma financiera vertical para el sector travel**.
