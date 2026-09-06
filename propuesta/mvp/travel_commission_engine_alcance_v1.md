# Travel Commission Engine — Alcance v1

> **Fuente normativa de alcance.** “Incluido en v1” significa comprometido para esa versión, no implementado en el árbol actual. El avance verificable se mantiene en el [roadmap](./roadmap.md); al 2026-09-04 solo existe una F1 parcial.

Documento que define qué es y qué no es la primera versión construible. El corte se organiza en bandas cercanas — v1, v1 Enterprise y v1.5 (ver §1) — porque algunas piezas (split host/advisor, RBAC, reconciliación) son requisito de venta, no lujo diferible. Toda la información proviene de los documentos existentes de esta serie; aquí solo se traza la línea de corte.

---

## Definición de producto

> Somos la capa especializada que recibe productos, ventas y contexto, aplica reglas versionadas y devuelve la comisión explicable de toda la cadena: cuánto debe generar la operación, cuánto corresponde a la agencia y cuánto al agente que la vendió. El agente lo sabe **antes de vender**: sube su producto, le aplica reglas o reutiliza las existentes, y cada producto muestra cuánto gana él y qué retiene la agencia.

No somos otro booking engine, ERP ni plataforma de pagos: somos la capa de cálculo embebible sobre los sistemas existentes del cliente.

### Las dos piezas del motor

En el fondo el producto son **dos motores encadenados** más una capa de explicación pegada a ambos (diagrama visual en `travel_commission_engine_dos_piezas.svg`):

```text
┌────────────────────┐   reglas    ┌────────────────────┐   nodos   ┌──────────────────┐
│ 1 · MOTOR DE REGLAS│────────────►│ 2 · MOTOR DE CÁLCULO│──────────►│ 3 · EXPLICACIÓN  │
│                    │             │                     │           │                  │
│ crear una vez,     │             │ recorre la cadena:  │           │ EvaluationTrace  │
│ reutilizar en N    │             │ Basis → Contrato →  │           │ por nodo:        │
│ · condiciones CEL  │             │ Override → Gross →  │           │ "€81 porque la   │
│ · jerarquía        │             │ Split → Neto        │           │  regla X, vers Y,│
│ · versionado       │             │ determinista,       │           │  contrato Z,     │
│ · 2 capas:         │             │ mismo antes y       │           │  base €2.250"    │
│   operación+split  │             │ después de vender   │           │ cadena auditable │
└────────────────────┘             └─────────────────────┘           └──────────────────┘
```

El motor de reglas y el de cálculo son lo que cualquier calculadora tiene; la pieza 3 (explicación por nodo) más el hecho de que la cadena sea **travel-native** son lo que convierte esto en infraestructura y no en calculadora.

**El motor de reglas tiene dos niveles de ambición, y solo el básico entra en v1:**

```text
MOTOR DE REGLAS
│
├─ BÁSICAS – INTERMEDIAS  (v1, ya incluidas)
│    outcome: percentage · fixed · tiered (tramos 70/80/90 por volumen)
│    2 capas: operación + split host/advisor (70/30, 80/20)
│    condiciones CEL · jerarquía · versionado + vigencia
│    → ya son reglas potentes: condicionales, jerárquicas, por tramos
│
└─ AVANZADAS  (v2+, agendadas, NO incluidas)
     STACK (apilar reglas) · split a N participantes
     agregación persistente + snapshots · rappels retroactivos
     consorcio de 3 niveles con cortes retenidos
     → lo verdaderamente complejo: acumulados, apilado, N partes
```

### Posicionamiento: no vendemos "cálculo de comisiones"

"El agente sabe cuánto ganará antes de vender" es una gran feature, pero **no es el diferenciador**. Ese terreno ya está ocupado: Salesforce Spiff ofrece Commission Estimator, statements en tiempo real, tracing, audit trails, effective dating, workflows, approvals, FX e integraciones; Varicent hace cálculo complejo, visibilidad en tiempo real, simulaciones, auditoría y administración de planes enterprise. Contra ellos no se compite diciendo "calculamos comisiones" ni siquiera "tus agentes saben antes de vender lo que ganan".

El diferenciador es el dominio, no la calculadora:

> **Commission infrastructure built for travel distribution.**
>
> From supplier contract to advisor payout, every commission explained.

Lo que un Spiff genérico no modela es la cadena de distribución travel: contrato de proveedor → override de consorcio → gross de la agencia → split host/advisor → payout, con componentes no comisionables por producto y conciliación contra statements de proveedor. Ahí no somos "un Spiff para agencias": somos el **source of truth de la obligación económica** de la distribución travel.

### El verdadero valor: no ahorramos trabajo, encontramos dinero

"Explicar cada euro" es una gran feature, pero por sí sola no es lo que hace que una red saque la tarjeta o firme un contrato grande. El comprador paga por la combinación completa —y sobre todo por lo último:

> **Sé cuánto debería cobrar + sé cuánto tengo que pagar + sé cuánto dinero me falta.**

El salto comercial es pasar de *"te ahorro trabajo administrativo"* a *"te encuentro dinero"*. Eso vive en un dashboard que no es un calculator, es un control de caja de comisiones:

```text
┌─────────────────────────────────────────────┐
│  Comisiones esperadas este mes    186.420 €  │
│  Cobradas                         171.730 €  │
│  Pendientes                        14.690 €  │
│  23 reservas requieren revisión              │
└─────────────────────────────────────────────┘
        cada línea → drill-down al nodo:
        qué contrato la esperaba y por qué falta
```

Por eso el pitch cambia según a quién mira. El **agente** recibe *"esta venta te deja 81 €"*; el **comprador económico** —Finance / COO / dirección de la red, no el agente— recibe algo más fuerte: *"sé exactamente cuánto debo cobrar, cuánto debo pagar y por qué"*. El agente entiende su gana; Finance encuentra el dinero que se estaba escapando. El economic buyer es el segundo, y el producto se posiciona hacia él.

Consecuencia de corte (ver §"El otro lado de la cadena"): una **conciliación básica por CSV** entra en v1, no en v1.5, precisamente para que la promesa "cuánto me falta" sea real desde el MVP. La sofisticación de la conciliación (import por API, disputes con workflow, matching AI) sí se difiere.

### El moat: no es una feature, es acumulación

La verticalización travel-native diferencia, pero por sí sola invita a la pregunta correcta: *"¿qué impide que otro haga lo mismo?"*. La defensa real no es una pieza única y copiable en un fin de semana; es **acumulativa**, y cada capa refuerza a las demás:

```text
modelo de datos travel-native (la cadena, la base comisionable)
  + biblioteca de reglas del sector (esquemas que ya vienen resueltos)
  + integraciones con los sistemas del cliente
  + histórico de contratos (qué rigió cada booking, versionado)
  + motor probado sobre millones de reservas (correcto y auditado)
  + matching de liquidaciones (esperado vs recibido, con fechas)
  + datos sobre comportamiento de proveedores (quién paga, cuánto, cuándo)
```

Ninguna capa aislada es un foso; juntas y con tiempo, sí. Y en la última aparece el salto de categoría: con histórico de contratos + matching de liquidaciones + comportamiento de proveedores, el motor puede afirmar lo que ninguna calculadora puede:

> "Este proveedor normalmente liquida a 47 días; tienes 38.000 € fuera de plazo."

En ese punto dejas de ser un commission calculator y pasas a ser una **financial intelligence layer para travel distribution**. Ahí está el techo grande del producto.

Disciplina de versión: v1 **no** construye esa inteligencia predictiva, pero pone deliberadamente los cimientos que la desbloquean — `supplier_contract` first-class y versionado, conciliación que registra esperado-vs-recibido **con fechas**, y el historial append-only. La vista de *aging* (vencido por proveedor) es la extensión natural en cuanto hay conciliación; la **predicción** del comportamiento del proveedor es el horizonte que solo la acumulación de datos habilita. Se nombra para fijar la dirección, no para prometerlo en el MVP.

### Encaje en el ecosistema

Este documento acota el **Commission Engine**, que es el módulo 1 (núcleo) de un ecosistema mayor (`../otras-fases/travel_sales_operations_ecosystem.md`): CRM, Product Hub, Quote Builder, Advisor Workspace, Supplier & Contract Hub, Reconciliation, Intelligence, Copilot, Automation, Consortium Hub e Integration Hub. El engine se vende **standalone** sobre los sistemas actuales del cliente, pero está diseñado para ser el servicio de cálculo compartido de todo el ecosistema:

```text
El motor es API-first y es la ÚNICA fuente de la lógica de comisión.
Quote Builder, Advisor Workspace, Intelligence y Copilot lo consumen;
ningún módulo hermano reimplementa el cálculo ni la cadena.
```

Consecuencia práctica para el corte: parte de lo que v1 declara "fuera, lo resuelve el cliente" no es del cliente para siempre — es un **módulo hermano posterior**. Donde eso cambia la lectura de la frontera, se marca abajo (catálogo → Product Hub; contrato → Supplier Hub; consorcio de N niveles → split N-party v2).

### Dos modos de consumo (el mismo motor, dos formas de venderlo)

El engine se entrega de dos maneras, sobre el **mismo binario y el mismo motor determinista**. No son dos productos: es una decisión de canal, no de código.

```text
MODO A · Plataforma (SaaS cloud)
  El cliente entra a nuestra app en la nube.
  Crea catálogo y reglas en la UI, registra ventas, consulta comisiones.
  Todo el producto, sin construir nada. Rampa: CSV/manual → API cuando quiera.
  Para: agencias, redes y consorcios que quieren una herramienta ya.

MODO B · Componente (embebido / integrado)
  El motor se integra donde el cliente YA trabaja: su booking engine,
  su back office, o el software de un fabricante (embed / white-label).
  Consumen la API; la UI la pone él, la ponemos nosotros, o ninguna.
  Corre en nuestro cloud (API) o self-hosted en su infra (mismo binario).
  Para: fabricantes de software travel y grandes con requisito on-prem.
```

Lo que **no** cambia entre modos: la cadena, el trace por nodo, el versionado, el cálculo determinista. Lo que cambia: **quién pone la UI y dónde corre el binario**. Un mismo cliente puede empezar en Modo A (plataforma, CSV) y migrar a Modo B (API embebida en su sistema) sin cambiar de motor ni reconfigurar reglas — la config viaja con él.

```text
                 ┌─────────────────────────┐
   Modo A  ─────►│                         │
  (nuestra UI)   │   COMMISSION ENGINE     │◄─────  Modo B
                 │   mismo binario, misma  │       (su UI / su sistema
   nuestro cloud │   API, mismo motor      │        / cloud o self-host)
                 └─────────────────────────┘
```

#### Un core, varias formas de consumo — y nuestra propia plataforma es el consumidor #1

Los dos modos son en realidad facetas de un mismo principio: **un core único con varios canales de consumo**, nunca tres motores distintos que evolucionan por separado. El consumidor más importante de ese core es **nuestra propia plataforma** (la suite del ecosistema): no debe repartir lógica de comisiones por su código, sino preguntarle al engine —`simulate` / `commit` / `cancel` / `explain`— exactamente como cualquier cliente externo. Ese *dogfooding* valida el motor y evita reimplementar la lógica cinco veces (CRM, cotizador, booking engine, back office, conciliación).

```text
                     COMMISSION ENGINE (core único)
                              │
        ┌─────────────────────┼─────────────────────┐
        ↓                     ↓                     ↓
   NUESTRA PLATAFORMA     COMMISSION CLOUD      API / COMPONENTE
   (la suite consume       (agencias entran      (software de terceros,
    el mismo engine)        directamente)         grandes / vendors)
   tenant = su cliente     tenant = agencia      tenant = cliente API
```

Esto no cuesta nada extra si se decide el día uno, y ya está decidido en v1: el motor es **API-first** y **multi-tenant** (`org_id` en todo, §Bloque 5). Cada llamada lleva su contexto (`tenant`, `product`, `booking`, `contract`, `effective_date`, `currency`, `amount`, `agent`), así que el mismo motor sirve idéntico a nuestra plataforma, al Cloud, a la API y a una instancia Enterprise. No se reinventa después.

Conviene separar **producto** de **motor**, con nombres y versión propios:

```text
Commission Engine    infraestructura: reglas · cálculo · versionado ·
                     simulación · reversión · ledger · explicación      v1
Commission Cloud     app SOBRE el engine: UI · usuarios · import Excel ·
                     dashboards · gestión de contratos · informes        v1
Commission API       canal formal para terceros: API keys · OAuth ·
                     webhooks · rate limits · sandbox · docs · SLA        v2
Commission Enterprise instancia dedicada / private cloud / on-prem para
                     "nuestros datos no salen de nuestra infra"           v2
```

El binario **puede** correr self-hosted desde v1 (mismo binario), pero *productizar* la API para terceros (keys, OAuth, sandbox, webhooks, docs, SLA) y el despliegue privado dedicado son oferta **v2**, no MVP. El mensaje comercial, en cambio, es simple desde el principio: *un motor único para calcular, explicar y conciliar comisiones de viajes; úsalo como Cloud, como API o como instancia Enterprise* — y la suite propia es, sencillamente, **"powered by Travel Commission Engine"**. Ventaja estratégica: un cliente que dice *"me gusta vuestro cálculo pero no cambio mi CRM"* no se pierde — se le vende solo el engine; otro que no quiere integrar API usa el Cloud; otro se lleva la suite completa. Un core, muchos streams, sin reconstruir tecnología.

El flujo central de la v1 es product-céntrico:

```text
1. El cliente sube sus productos al catálogo
   (incluidos paquetes compuestos por componentes)
   y define qué parte de cada uno es comisionable
2. Crea reglas una vez y las aplica / reutiliza en N productos,
   tanto de operación (proveedor / consorcio) como de reparto (agente)
3. Cuando un agente mira lo que va a vender,
   ya ve su neto y el resto de la cadena: el cálculo lo hace el motor
4. La venta se registra y el cálculo queda confirmado
5. Si se cancela, la reversión del saldo es automática
```

Ni creamos reservas, ni pagamos, ni contabilizamos formalmente, ni asesoramos fiscalmente. Capa de cálculo embebible sobre los sistemas existentes del cliente.

## La cadena económica

El mayor cambio respecto a la calculadora original: en travel no hay una sola relación económica, hay dos encadenadas.

```text
Proveedor → Agencia / Host / Consorcio
Agencia / Host → Advisor / Agente / Sucursal
```

Alcance honesto del consorcio: v1 modela **dos** relaciones encadenadas y el override del consorcio se trata como un nodo que ajusta el gross de la agencia (`+X%`). Cuando el consorcio **retiene** su propio corte — supplier → consorcio → agencia → advisor, tres partes cada una con lo suyo, como en el Consortium Hub del ecosistema (`Supplier 10% · Consortium +3% · Agency 2% · Advisor 80%`) — eso es un split a **N participantes**, y sigue siendo v2 (`Bloque 2 · El split`). La cadena de v1 cubre proveedor→agencia y host→advisor; no los tres cortes simultáneos retenidos. Se dice para no sobreprometer el caso consorcio con el modelo de dos hops.

Por eso una reserva de €10.000 nunca es simplemente `€10.000 × 10%`. Puede haber booking total, menos impuestos, menos elementos no comisionables, commissionable amount, supplier commission, override/bonus del consorcio, agency gross commission, split del advisor, bonus/adjustment y advisor payable. El motor ya no calcula `amount × percentage`; recorre la cadena completa:

```text
Booking Amount
      ↓
Commissionable Basis        ← impuestos, fees y componentes no comisionables fuera
      ↓
Supplier Contract           ← comisión del proveedor
      ↓
Override / Bonus consorcio
      ↓
Agency Gross Commission
      ↓
Agent Split                 ← advisor / sucursal / franquicia
      ↓
Bonuses / Adjustments
      ↓
Net Agent Commission
      ↓
Accrued → Payable → Paid
```

Ejemplo:

```text
Booking total               €10.000
– no comisionable           –€1.000   (impuestos, fees)
Commissionable basis         €9.000
Supplier commission 12%      €1.080
Override consorcio +2%         €180
Agency gross commission     €1.260
Split advisor 30%             €378   ← lo del agente
Bonus / ajustes               +€50
Net agent commission          €428
```

Cada nodo produce su propio **EvaluationTrace**. La explainability deja de ser un adorno del resultado y pasa a ser la columna vertebral del producto: cuando Finance, operaciones o el agente cuestionen un número, la respuesta es la cadena entera, nodo por nodo — qué regla, qué versión, qué precedencia y qué base provocaron cada importe.

Esto es dominio travel, no contabilidad genérica: ASTA seguía tratando en 2026 los componentes no comisionables de cruceros y cómo modifican la base real con la que se remunera al advisor, y los hoteles definen explícitamente qué tarifas y componentes son comisionables, excluyendo impuestos y fees. El moat vive aquí: quien modela solo `amount × %` se queda en calculadora; quien modela la cadena construye infraestructura.

Un corte honesto: la cadena tiene dos mitades. La del **"debe"** — cuánto *debe* generar la operación, cuánto *corresponde* a la agencia, cuánto *corresponde* al agente — entra completa en v1. La del **"haber real"** — cuánto pagó de verdad el proveedor a la agencia y, por tanto, cuánto queda realmente por pagar al agente — es la conciliación: su **forma básica (CSV) entra en v1**, y su forma rica (import por API, disputes, drill-down) llega en **v1.5**.

Esto importa decirlo claro, porque es justo la frontera entre calculadora e infraestructura. En v1, el ciclo del "debe":

```text
Net Agent Commission → ACCRUED → PAYABLE → PAID (marcado a mano)
```

registra lo que la agencia **debería** pagar al agente según las reglas. Marcar `PAID` a mano cierra el ciclo contra el saldo del agente. Cruzar ese devengado esperado contra lo que el proveedor **efectivamente ingresó** es la conciliación básica CSV que v1 ya incluye (§"El otro lado de la cadena"); lo que v1.5 añade es la sofisticación, no el concepto.

## El otro lado de la cadena: reconciliación (básica en v1, rica en v1.5)

El dolor no termina en "¿cuánto debería ganar?". El dolor completo del back office es: *¿cuánto debería haber ganado? ¿cuánto debería haber recibido la agencia? ¿cuánto recibimos realmente? ¿qué falta? ¿qué le corresponde al advisor? ¿por qué?* Tern, por ejemplo, está construyendo justo ese back office — reconciliar statements de proveedores, detectar comisiones no recibidas, ajustar splits y gestionar payouts de advisors, con matching asistido por AI entre statements y bookings ya en 2026. El mercado señala que el source of truth de la obligación económica vale más que la calculadora.

Por eso la conciliación no espera entera a v1.5: su forma mínima es tan barata sobre lo que v1 ya tiene —el `external_id` de cada booking, el importe esperado que sale del cálculo— que dejarla fuera sería regalar el argumento comercial más fuerte. La línea de corte no es "reconciliación sí / no", es "reconciliación básica CSV (v1) / reconciliación rica (v1.5)":

```text
v1  · básica (CSV)      Subo el CSV de liquidación del proveedor
                        → matching por external_id / referencia
                        → COBRADO / FALTA / DIFERENCIA por booking
                        → marcar conciliado a mano
                        Hace real la promesa "cuánto me falta".

v1.5 · rica             Import por API (no solo CSV)
                        → drill-down pulido al nodo que esperaba el importe
                        → disputes con estados (OPEN/REVIEWING/RESOLVED)
                        → excepciones y evidencia
v2  · AI                Matching asistido por AI de statements
```

No nos convertimos en banco ni PSP. Pero sí somos quien sabe, por cada booking, lo que **debía** entrar y puede cruzarlo contra lo que **entró**:

```text
EXPECTED COMMISSION     $1.350
RECEIVED                $1.200
──────────────────────────────
UNRECONCILED              $150

click →  Supplier:            X
         Booking:            ABC123
         Expected rate:      15%
         Commissionable base: $9.000
         Expected:           $1.350
         Received:           $1.200
         Variance:           –$150   ← con trace del nodo que lo esperaba
```

El dashboard de arriba es exactamente ese cruce, agregado por mes: esperado vs recibido vs pendiente, con drill-down a la línea. En v1 el statement entra por CSV y se marca conciliado a mano; el resto (API, disputes con estados, matching AI) es el corte v1.5/v2 de arriba.

Esto se vende a Finance y Back Office mucho mejor que un calculator: no es "cuánto calcula", es "cuánto le falta cobrar y a quién se lo debe".

## Qué somos / qué no somos

| Sí somos | No somos | Quién resuelve |
|---|---|---|
| Motor de la cadena de comisiones (operación → agencia → agente) | Sistema de reservas | Su booking engine |
| Catálogo comercial ligero de productos | Inventario: disponibilidad ni precios | Sus sistemas de venta |
| Configuración flexible de reglas | Plataforma de pagos | Su banco/PSP/nómina |
| Trazabilidad "¿por qué este importe?" | Sistema contable oficial | Su ERP/asesoría |
| Consulta de devengos por agente | Declaraciones fiscales | Sus gestores |
| Contrato de proveedor como entidad de primera clase | Firmar/negociar el contrato | El cliente y su proveedor |
| Source of truth de la obligación económica (expected vs received) | Cobrar o pagar el dinero (banco/PSP) | Su tesorería; matching AI en v2 |

El catálogo guarda lo necesario para calcular (tipo, proveedor, base comisionable, atributos comerciales). No gestiona disponibilidad, tarifas de venta ni cupos: eso vive en los sistemas del cliente.

Matiz sobre la columna "quién resuelve": para el engine standalone, hoy lo resuelve el cliente o su proveedor. En la visión del ecosistema, varias de esas filas las absorbe un **módulo hermano** (Product Hub, Supplier Hub, CRM, Reconciliation) — no dejan de ser "no engine", pero pasan a ser "otro módulo nuestro", no "para siempre del cliente".

---

# 1. Principio rector del corte

> Las decisiones estructurales entran el día uno porque son decisiones, no construcción. La maquinaria pesada se difiere sin romper nada si aquellas están tomadas.

## Bandas de versión en este documento

```text
v1            motor de la cadena completa: basis + supplier contract +
              override + gross + split host/advisor + tiered básico + neto
              + conciliación básica CSV (expected vs received vs pendiente)
v1 Enterprise RBAC por rol + approvals (maker-checker). No es v2 lejana:
              sin esto no se cierra un enterprise
v1.5          conciliación rica: import API + disputes/exceptions con
              estados + drill-down al nodo
v2+           maquinaria pesada: agregación persistente, ledger doble
              partida, PSP/payouts automáticos, matching AI, FX, eventos
```

Estructural (día uno, casi gratis, brutal de retrofitar):

```text
org_id en todas las entidades          (multi-tenant, §21 arquitectura)
dinero como par {valor, divisa}        (nunca float, §5)
customer_ref opaco                     (cero PII ingerida)
supplier_contract entidad propia       (comisión nace del contrato, no de un %)
versionado de reglas + vigencia       (§23)
evaluation_trace por cálculo Y nodo    (jerarquía + cadena económica)
enums del ciclo de vida correctos      (liquidación)
append-only en movimientos             (§22 simplificado)
rol por usuario                        (habilita RBAC/approvals sin retrofit)
```

Maquinaria (se difiere a v2+): doble partida, bus de eventos, KMS, PSPs, motor de agregación persistente, matching AI. RBAC completo y approvals **no** se difieren a v2: entran como v1 Enterprise.

---

# 2. Los cinco bloques de la v1

## Bloque 1 · Catálogo de productos

El cliente sube sus productos (negocio §1):

```text
Hoteles · Vuelos · Paquetes · Actividades
Cruceros · Seguros · Transfers · Rent-a-car

Alta: formulario guiado + CSV + API

Atributos mínimos por producto:
  type, code, name, supplier_ref,
  base comisionable           (ver abajo: pct plano O exclusiones por línea)
  attributes JSONB (destino, categoría, ...)
```

El catálogo guarda lo necesario para calcular: tipo, proveedor, base comisionable y atributos comerciales. Sin disponibilidad ni tarifas de venta.

> **Frontera con el Product Hub.** Este catálogo ligero es la **semilla del Travel Product Hub** (módulo 3 del ecosistema), no un competidor suyo. Precio de venta, promociones, popularidad y conversión son del Product Hub más adelante; hoy, mientras ese módulo no existe, esos datos viven en los sistemas del cliente. v1 solo modela lo que el cálculo necesita: no añadir campos comerciales aquí es deliberado, para no duplicar lo que será el Product Hub.

### Base comisionable: dos formas, no una

Un solo `commissionable_pct` plano no basta para el caso que usamos como bandera. El ejemplo de cruceros de ASTA es exactamente el de componentes no comisionables **dentro de un mismo producto** (NCFs, port charges, taxes sobre una tarifa de crucero), y un hotel excluye tasas y fees de su propia tarifa. Eso no es un paquete de varios productos: es un producto con líneas que sí comisionan y líneas que no.

Por eso en v1 la base comisionable de un producto simple se declara de una de dos maneras:

```text
1. commissionable_pct          % plano sobre el importe
   ej.: renta de auto, 90% comisionable
2. non_commissionable_lines    exclusiones nombradas por línea
   ej.: crucero → { fare: comisionable,
                    ncf | port_charges | taxes: fuera de base }
```

La forma 2 permite que un producto no-paquete tenga base por componentes sin obligar a modelarlo como paquete. El booking, si trae desglose real de sus líneas, manda sobre la declaración del producto (§Bloque 3). Ambas formas producen el mismo nodo `Commissionable Basis` con su trace: la diferencia es solo cómo se declara la exclusión, no cómo se explica.

### Paquetes compuestos

Un paquete es un producto que agrupa componentes del propio catálogo:

```json
{
  "type": "package",
  "name": "Cancún Todo Incluido",
  "components": [
    { "product_ref": "vuelo-mad-cun" },
    { "product_ref": "hotel-riu-cun" },
    { "product_ref": "transfer-cun"  }
  ]
}
```

Cada componente calcula su comisión con sus propias reglas;
la comisión del paquete es la suma explicada, línea por línea.
Para agencias empaquetadoras este feature justifica solo el producto:
sin él tendrían que meter cada paquete con un % plano a mano.

### Supplier Contract: entidad de primera clase

La comisión no nace de un porcentaje suelto: nace de un **contrato comercial** con el proveedor. Por eso `supplier_contract` deja de ser una simple dimensión de la jerarquía y pasa a ser una entidad principal del sistema, con vida propia y versionado:

```text
supplier_contract
  supplier_ref, name, currency
  valid_from / valid_to            (vigencia como cualquier regla)
  commission_terms                 (tasa base, overrides, exclusiones de base)
  markets / products cubiertos
  status: draft | active | expired
```

Beneficios de tratarlo como first-class y no como un atributo:

```text
· El nodo Supplier Contract de la cadena referencia SU contrato y versión:
  "esta comisión salió de este contrato, vigente en esta fecha"
· La conciliación (básica en v1) cruza el statement contra el contrato esperado
· Renegociar un contrato es una versión nueva, no reescribir reglas sueltas
· Auditoría: qué contrato regía cada booking histórico, sin ambigüedad
```

Las reglas de la capa `operation` se apoyan en el contrato; el contrato es el "por qué" de la tasa antes incluso de que intervenga cualquier override.

> **Frontera con el Supplier & Contract Hub.** El `supplier_contract` de v1 es la **semilla del Supplier & Contract Hub** (módulo 6, Fase 1 núcleo del ecosistema), no el módulo completo. v1 modela el contrato **hasta donde el cálculo y la reconciliación lo necesitan**: términos comisionables, vigencia y versión. La superficie de gestión rica — negociación, metas, promociones, comparativas entre proveedores, alertas de renovación — es el módulo hermano. Delimitarlo evita que el engine se convierta por goteo en un CRM de proveedores.

## Bloque 2 · Reglas reutilizables sobre productos

De `travel_commission_engine_jerarquia_reglas.md`:

```text
Crear una regla una vez, aplicarla a N productos.
O crearla desde un producto y reutilizarla en otros.

Dos capas de reglas sobre la misma maquinaria (la cadena económica):
  operation → comisión de la operación: proveedor + overrides/bonus
              base = commissionable amount
  split     → reparto agencia → advisor / sucursal / franquicia
              base = gross commission de la capa anterior

Capas ≠ nodos. Dos capas de *reglas*, pero la cadena de la sección
"La cadena económica" tiene más nodos, y cada uno traza aparte:

  operation → Supplier Contract     (nodo propio, su regla/versión)
            → Override / Bonus       (nodo propio, no un caso del anterior)
  split     → Agent Split            (nodo propio)
            → Bonuses / Adjustments  (nodo propio)

Jerarquía fija por dimensiones:
market < country < channel < agency < branch <
agent < supplier < product < contract

Asociación explícita regla ↔ producto,
más herencia por jerarquía (sucursal, agente).

Condiciones SI/ENTONCES en CEL
Outcome: percentage | fixed | tiered (sobre SU base según capa)
         tiered = tabla de tramos, ej.:
           0–100k = 70% · 100k–300k = 80% · >300k = 90%
Composición: FIRST_MATCH (la más específica gana)
Empate exacto = error bloqueante
Versionado + valid_from/valid_to
EvaluationTrace persistido por cálculo y por nodo
Validación pre-activación: lint + simulación manual
```

La decisión importa porque el moat es la explicabilidad **por nodo**: si el override del consorcio quedara subsumido dentro de "la comisión de operación", el agente no podría preguntar "¿de dónde salió este +2%?" y recibir un nodo con su regla, su versión y su precedencia. Colapsar la cadena en dos importes (operación y split) sería volver a la calculadora con más pasos. Dos capas de reglas para configurar; la cadena entera de nodos para explicar.

Ejemplo de reutilización (negocio §3): la regla "Rent-a-Car 10%" se aplica de golpe a los 120 autos del catálogo; luego "Renta Carros Cancún 12%" pisa solo esos. En la cadena: AUTO_BASE es nodo Supplier Contract de la capa `operation`; ADVISOR_30 es nodo Agent Split de la capa `split`.

### Onboarding de reglas: el importador de Excel hace real la promesa

La siguiente pregunta del cliente después de "define productos, ventas y condiciones" es siempre **"¿cómo?"**. Si tiene que teclear 700 contratos a mano y mantenerlos él, solo hemos movido su Excel a otra interfaz. Por eso el importador no es un accesorio de demo: es lo que separa una demo bonita de un producto adoptable, y sostiene la frase del pitch *"suban su Excel de siempre"*.

```text
Excel del cliente (contratos / tarifas / splits)
        ↓  importador
Reglas PROPUESTAS (borrador, no publicadas)
        ↓  el usuario revisa y ajusta
Publicar versión  → reglas activas y versionadas
```

Alcance v1 del importador: mapear columnas del Excel a reglas de las capas `operation` y `split`, generar reglas en estado borrador, dejar que el usuario las revise contra el guardián de cobertura, y publicar una versión. Lo que se difiere: extracción de reglas desde un contrato en PDF por AI (v3, capa AI). El importador convierte el "sube tu Excel" de eslogan en flujo real.

### El split no espera a v2: es el corazón del modelo host/advisor

La capa `split` es v1 obligatorio, no una mejora futura. Las host agencies **son** un split entre host y advisor: modelos públicos como 70/30 y 80/20 según producción son el caso de uso central, no una excepción. Un motor de comisiones travel que no reparte host→advisor en v1 no sirve al segmento que más lo necesita.

```text
Supplier commission → Agency gross → Split host/advisor → Advisor payout
   contrato proveedor    lo de la agencia   70/30, 80/20…      neto advisor
```

En v1 el split es a un beneficiario (host retiene, advisor cobra). El reparto a **N participantes** simultáneos (p. ej. advisor + sucursal + referidor en una misma venta) sigue siendo v2.

### Tiered básico también entra en v1

Los esquemas progresivos por volumen (70% hasta 100k, 80% hasta 300k, 90% por encima) tienen demasiado valor para diferirlos. No hace falta el motor de agregación definitivo: el outcome `tiered` es una **tabla de tramos** que selecciona el porcentaje según un volumen de entrada (pasado en el contexto o resuelto por un query simple sobre los bookings del periodo). Lo que se difiere a v2 es el motor de agregación **persistente** con snapshots y recálculo retroactivo (rappels), no la tabla de tramos.

### Guardián de cobertura

```text
⚠️ Producto sin ninguna regla aplicable
   → marcado como "comisión no configurada"
   → nunca se calcula €0 en silencio
```

Aplica a ambas capas: un producto con operación configurada pero reparto sin definir también se marca — nunca se inventa un split.

Un query trivial que evita el error más caro y silencioso del negocio: vender con comisión cero por descuido.

Fuera de v1: STACK, splits a N participantes, tiered con agregación persistente (v2) · agregados persistentes y period rules / rappels retroactivos (v2).

## Bloque 3 · Cálculo: antes y después de vender

Firma del motor (§7 arquitectura):

```text
Calculate(Producto | Paquete | Booking, Rules, Context)
    -> CalculationResult        // cadena completa, trace por nodo
```

El mismo motor determinista se expone como **dos verbos explícitos**, no como dos endpoints casuales. Esta distinción es la que hace embebible el motor:

```text
SIMULATE   ¿qué pasaría si vendo esto?   NO genera saldo ni movimiento.
           preview, quote, cotización.   Misma cadena, mismo trace.
COMMIT     la venta ocurrió.             Genera los movimientos append-only
           registro de booking.          y confirma el cálculo (idempotente).
```

`SIMULATE` es lo que consume un booking engine, un CRM, un cotizador o un marketplace para mostrar la comisión antes de cerrar; `COMMIT` es lo que llama el sistema del cliente cuando la venta se confirma. Mismo motor, mismo resultado; lo único que cambia es si el cálculo deja rastro económico. Nombrarlos así desde v1 es lo que sostiene la futura estrategia API (Modo B).

Antes de la venta — `SIMULATE`, la pregunta clave del agente:

```text
GET /v1/products/{id}/commission?agent=carlos&amount=2500

Base comisionable         €2.250   (el producto excluye tasas: 90%)
Operación AUTO_BASE 12%     €270
Split ADVISOR_30%            €81
─────────────────────────────────
Neto agente                  €81  ·  agencia retiene €189

→ "Si vendes este auto a €2.500, ganas €81"
   con trace completo nodo por nodo
```

Para un paquete: cada componente recorre su propia cadena y el split se aplica sobre el gross explicado:

```text
GET /v1/products/paquete-cun/commission?agent=carlos&amount=3200

Vuelo     € 900 × 5%    =  € 45
Hotel     €2.100 × 12%  = €252
Transfer  € 200 fijo    =  € 10
─────────────────────────────────
Gross paquete €3.200    = €307   ← trace por componente:
                                  qué regla pagó cada línea
Split advisor 30%         –€92.10
Neto agente              €214.90  ← trace por componente Y por nodo
```

Después de la venta — `COMMIT`, confirmación:

```text
POST /v1/bookings            API desde su sistema
Importación CSV / manual     para volúmenes pequeños

Idempotencia por external_id:
reenviar una venta nunca la duplica
```

```text
✓ Función pura y determinista: mismo motor en preview y venta
✓ Redondeo explícito HALF_UP, precisión 2      (§5)
✓ Divisa siempre acompañando al importe         (§5bis)
✓ evaluation_trace por nodo: basis → contrato → reparto → neto (§17 negocio)
✓ La misma entrada con la misma versión de reglas
  produce siempre el mismo resultado
```

La base comisionable es parte del input congelado del cálculo: manda la declaración del producto (pct plano o exclusiones por línea, §Bloque 1); si el booking trae desglose real de sus líneas, ese desglose. Cambiar la declaración de un producto no reescribe cálculos históricos.

Operación mono-divisa en v1: los importes llevan divisa desde el día uno, pero no hay conversión automática (FX versionado → v2). Un cliente opera en su divisa.

Sin PII: ni nombres de viajeros ni contactos. Solo referencias opacas.

## Bloque 4 · Lo que ve el agente

La promesa v1: cuando el agente mira lo que va a vender, ya sabe cuánto gana y de dónde sale cada euro.

Endpoints (subconjunto de §8 arquitectura):

```text
GET /v1/products                        catálogo con MI comisión
GET /v1/products/{id}/commission        detalle + explainability
GET /v1/calculations/{id}               cualquier cálculo histórico
GET /v1/commissions?agent=&period=      lo devengado por periodo
GET /v1/balances?agent=                 saldo pendiente
GET /v1/agents/{id}/statement?period=&format=csv
                                        estado de cuenta exportable
```

El estado de cuenta mensual (ventas, comisiones, ajustes, saldo)
es lo primero que el agente espera recibir y lo que la red usa
para pagar. Consulta + formato: casi gratis, muy visible.

Respuesta de explainability (formato §17 del documento de negocio):

```text
Neto agente: €81 sobre renta de auto
← Basis: €2.500 → comisionable €2.250 (producto excluye tasas)
← Regla RENTA_CAR_BASE ganó en operación (product.type = car): 12% = €270
← CANCUN_OVERRIDE quedó fuera por precedencia
← Split ADVISOR_30: €81 · la agencia retiene €189
```

Webhooks y notificaciones push → v2. En v1 el cliente consulta por API o UI.

## Bloque 5 · Multi-tenant

```text
organization_id en TODAS las entidades      (§21)
Aislamiento forzado en repositorio:
el org_id viene del token, jamás del body
Una agencia o consorcio = una organización
El mismo binario sirve SaaS y self-hosted   (§33)
```

---

# 3. Modelo de datos v1

Subconjunto mínimo derivado de las entidades del documento de negocio (§16) y ejemplos de arquitectura:

```sql
organization, agent, branch, supplier
supplier_contract            -- first-class: términos + vigencia + versión
product                      -- catálogo ligero + commissionable_pct / exclusiones
rule_product                 -- asociación reutilizable regla ↔ producto
booking                      -- venta registrada; importes explícitos (ver abajo)
commission_rule              -- scope JSONB + CEL + outcome (pct|fixed|tiered) + versión + capa (operation|split)
commission_calculation       -- input, desglose por nodo (basis/supplier/override/gross/split/bonus/neto), trace, rule_version
movement                     -- append-only por beneficiario
job                          -- CSV y tareas asíncronas

-- v1 (conciliación básica CSV)
supplier_statement           -- statement importado por CSV, por proveedor/periodo
statement_line               -- línea del statement, con match a booking + received_amount
reconciliation               -- expected vs received vs variance por booking/línea

-- v1.5 (conciliación rica)
dispute                      -- OPEN|REVIEWING|RESOLVED + comentarios + evidencia
```

Los importes son explícitos y no derivados de un único total (suf. para basis, margen y conciliación):

```text
booking:  gross_amount, commissionable_amount, net_margin,
          component_amount[]        (por línea, para base parcial y paquetes)
calculation nodo: supplier_commission, override_amount, agency_gross,
          split_amount, bonus_amount, net_agent_commission
```

La entidad `product` ya existía en el modelo de negocio (§16); la v1 la convierte en ciudadana de primera clase porque el cálculo previo parte de ella. `supplier_contract` sube al mismo rango: la tasa nace del contrato, no de un porcentaje suelto. La cadena completa (basis → supplier → override → gross → split → bonus → neto) vive dentro de `commission_calculation`: un registro por venta, varios nodos, cada uno con su trace.

## Saldos v1 (sin doble partida)

La tabla `movement` registra entradas append-only por beneficiario:

```text
CALCULATED +425 · ADJUSTMENT -25 · BONUS +50 · CLAWBACK -100
```

El saldo se deriva de los movimientos. Es exactamente el log del §22 original, suficiente para responder "¿cuánto le debo al agente X?". El subledger formal en doble partida (`../otras-fases/travel_commission_engine_ledger_doble_partida.md`) llega en v2 cuando existan settlements automáticos.

Conceptualmente, este log append-only es la pieza que falta entre "calcular" y "explicar": el **ledger de comisiones**. Calcular una comisión no basta; hay que saber qué le pasó después —reserva creada `+81 €`, modificada `−12 €`, cancelada `−69 €`, liquidación del proveedor, pago al agente. Ese historial inmutable de movimientos es lo que convierte el sistema de *calculator* en *source of truth*. No hace falta contabilidad de doble partida en v1 para tenerlo; basta el log. Así, las piezas del motor se leen mejor como cuatro: **1 · contratos/reglas → 2 · cálculo → 3 · ledger de comisiones → 4 · explicación/auditoría**.

## Ciclo de vida v1

Los estados existen desde el día uno porque son un enum, no maquinaria (de `../otras-fases/travel_commission_engine_liquidacion_settlements.md`):

```text
ESTIMATED → CONFIRMED → ACCRUED → PAYABLE → PAID
CANCELLED · DISPUTED · ADJUSTED · CLAWBACK
```

En v1 las transiciones son manuales o por API (el cliente confirma checkout desde su sistema); PAID se marca a mano o exportando el lote. Sin batches automáticos ni PaymentProvider (v2).

## Reversión automática por cancelación

En travel se cancela constantemente; la v1 lo resuelve de serie:

```text
Venta marcada CANCELLED
    → movimiento negativo append-only
      referenciando la venta original
    → el saldo del agente baja solo
    → ningún registro histórico se toca
```

Coste casi cero sobre la estructura existente (movimientos
append-only + enum CLAWBACK) y elimina la tarea manual
más odiada del mes contable de una red de agencias.

## Disputes / exceptions (v1.5)

Cuando la reconciliación encuentra una brecha (expected ≠ received) o un agente cuestiona un importe, hace falta un lugar donde llevarlo — pero **no** un Jira dentro del sistema. Basta un flujo mínimo sobre la cadena que ya existe:

```text
Estados:    OPEN → REVIEWING → RESOLVED
Adjuntos:   comentarios + evidencia (referencia opaca al doc, sin PII)
Vínculo:    dispute referencia el booking / la línea de reconciliación
            y el nodo de la cadena en discusión
Cierre:     RESOLVED puede disparar un ADJUSTMENT / CLAWBACK append-only
```

El valor está en que la disputa apunta al **nodo** con su trace: no se discute "un número", se discute "el override del contrato X en su versión Y". El workflow completo con roles, SLAs y offset automático de clawback sigue siendo v2.

## Eventos v1

Cálculo síncrono vía API. Tabla `job` para CSV y tareas asíncronas con idempotencia por clave de negocio. NATS, outbox relay y webhooks → v2 (`../otras-fases/travel_commission_engine_eventos_consistencia.md` define exactamente cómo crecer cuando llegue el momento).

---

# 4. Seguridad y datos en v1

De `../otras-fases/travel_commission_engine_proteccion_datos.md` (su propuesta minimalista entra completa):

```text
customer_ref opaco: cero PII ingerida
Validación anti-PII en metadata en modo warn
Sin vault: no hay datos personales que custodiar
Export DSAR trivial porque casi no hay datos
```

De autorización, la base estructural entra en v1:

```text
API key por organización
Rol por usuario desde el día uno (estructural, sin retrofit)
Audit log de cambios de reglas: quién, cuándo, qué versión
```

RBAC completo + approvals (maker-checker) **no se difieren a un v2 lejano**: son v1 Enterprise. Un solo admin con API key no cierra un enterprise — Finance exige separación de funciones (quien crea una regla no es quien la aprueba) y control de acceso por rol. Como el rol por usuario es estructural desde v1, activar RBAC y el flujo de aprobación es configuración, no reescritura:

```text
v1 Enterprise:
  RBAC por rol (admin / finance / ops / advisor read-only)
  Approval maker-checker en activación de reglas y contratos
break-glass, workflows largos y SoD avanzado → v2
```

---

# 5. Stack de la v1

Recorte del stack recomendado (§31 y §28) a lo operable por un equipo mínimo:

```text
Go                    core monolito modular
PostgreSQL            única base de datos
OpenAPI REST          contrato público
CEL                   condiciones de reglas
React                 editor de reglas + catálogo con comisión
                      por producto + vista del agente
Docker Compose        despliegue único, SaaS y self-hosted
YAML + env            configuración
Logs estructurados + OTel básico
```

Explícitamente fuera de v1:

```text
NATS / Kafka          jobs table basta a este volumen
Kubernetes / Helm     Docker Compose primero
Redis                 sin caché mientras no duela
Temporal              workflows largos → v3
KMS / Vault           sin PII cifrada que gestionar
```

La pieza diferencial de la UI en v1 es la facilidad de configuración prometida: formularios guiados por la jerarquía, plantillas iniciales y vista previa de cálculo sobre una reserva de ejemplo usando el mismo motor (reutiliza explainability; el simulador avanzado sobre corpus históricos es v2-v3 según `travel_commission_engine_jerarquia_reglas.md` §10).

---

# 6. Fuera de alcance v1 (con destino)

Ya **dentro** de v1 / v1.5 (antes diferido): split host/advisor, tiered básico por tramos, supplier contract first-class, conciliación básica CSV expected-vs-received (v1), importador de reglas desde Excel (v1), conciliación rica + disputes (v1.5), RBAC + approvals (v1 Enterprise).

Lo que queda fuera y su destino:

```text
STACK, splits a N partes                v2   reglas_agregacion + jerarquía
Tiered con agregación persistente       v2   reglas_agregacion
Motor de agregación persistente         v2   reglas_agregacion
Period rules, rappels retroactivos      v2   reglas_agregacion
Matching AI de statements               v2   negocio §6 (recon. avanzada)
Disputes: workflow + offset automático  v2   liquidacion_settlements §7-8
break-glass + SoD avanzado              v2   autorizacion_aprobaciones
FX versionado y conversión              v2   arquitectura §5bis
Outbox + NATS + webhooks                v2   eventos_consistencia
Ledger doble partida                    v2   ledger_doble_partida
Batches + PaymentProvider               v2   liquidacion_settlements
Módulo fiscal                           v3   impuestos
Crypto-shredding + vault                v3   proteccion_datos
Connectores REST/DB/CDC genéricos       v3   arquitectura §10-11
Kafka adapter, Temporal, K8s/Helm       v3   arquitectura §13, 24, 18
AI layer (contrato→reglas, copilot)     v3   arquitectura §26-27
Simulador what-if sobre histórico       v3   jerarquía §10
```

---

# 7. Clasificación completa por componente

| Componente | Versión | Documento de referencia |
|---|---|---|
| Catálogo de productos multi-vertical | v1 | negocio §1, §16 |
| Paquetes compuestos (cálculo por componente) | v1 | negocio §1 |
| Cadena económica: basis → contrato → gross → split → neto | v1 | definición, "La cadena económica" |
| Commissionable basis (impuestos/fees/componentes fuera de base) | v1 | negocio §16; ASTA cruceros 2026 |
| Reglas reutilizables regla ↔ producto | v1 | negocio §2-3, jerarquia_reglas |
| Importador de reglas desde Excel (borrador → publicar versión) | v1 | negocio §1 (onboarding) |
| Guardián producto-sin-regla | v1 | jerarquia_reglas §7 (validación) |
| Reversión automática por cancelación | v1 | liquidacion_settlements §8 (simplificado) |
| Cálculo previo: ¿cuánto gano si vendo esto? | v1 | negocio §4, §17 |
| SIMULATE / COMMIT: verbos del motor (preview sin saldo / venta) | v1 | stack_arquitectura §7-8 |
| Estado de cuenta exportable por agente | v1 | negocio §7 |
| Modelo canónico mínimo | v1 | stack_arquitectura §14 |
| Dinero `{valor, divisa}`, sin float | v1 | stack_arquitectura §5, §5bis |
| Multi-tenant org_id + aislamiento | v1 | stack_arquitectura §21 |
| Jerarquía de dimensiones + ranking | v1 | jerarquia_reglas §2-3 |
| FIRST_MATCH + empate = error | v1 | jerarquia_reglas §4-5 |
| Condiciones CEL | v1 | stack_arquitectura §6 |
| Outcomes percentage / fixed / tiered (tramos) | v1 | reglas_agregacion §5 |
| Split host/advisor (70/30, 80/20) | v1 | jerarquia_reglas; host agency |
| Supplier Contract entidad first-class | v1 | negocio §16 (elevado) |
| Commission basis con importes explícitos | v1 | negocio §16 |
| Versionado de reglas + vigencia | v1 | stack_arquitectura §23 |
| EvaluationTrace / explainability | v1 | jerarquia_reglas §6, negocio §17 |
| Registro de ventas API + CSV + manual | v1 | negocio §15, §18 |
| Saldos append-only simples | v1 | ledger_doble_partida §1 (log plano) |
| Enums ciclo de vida | v1 | liquidacion_settlements §2 |
| Marcado manual de pago | v1 | liquidacion_settlements §11 |
| customer_ref sin PII + metadata warn | v1 | proteccion_datos §3-4 |
| Audit log básico | v1 | autorizacion_aprobaciones §2 |
| Go + Postgres + React + Compose | v1 | stack_arquitectura §31 |
| RBAC por rol + approvals (maker-checker) | v1 Enterprise | autorizacion_aprobaciones §2-6 |
| Conciliación básica CSV (expected vs received vs pendiente) | v1 | negocio §6 (básico) |
| Conciliación rica (import API + drill-down al nodo) | v1.5 | negocio §6 |
| Disputes básicos (OPEN/REVIEWING/RESOLVED + evidencia) | v1.5 | liquidacion_settlements §7 (simplificado) |
| STACK / splits a N partes / tiered con agregación | v2 | reglas_agregacion §5, jerarquia §4 |
| Motor de agregación + snapshots | v2 | reglas_agregacion §3, 9 |
| Period rules (rappels retroactivos) | v2 | reglas_agregacion §6 |
| FX versionado + conversión | v2 | stack_arquitectura §5bis |
| Outbox + NATS + idempotencia | v2 | eventos_consistencia §2-7 |
| Webhooks salientes | v2 | stack_arquitectura §16 |
| Ledger doble partida | v2 | ledger_doble_partida §2-6 |
| break-glass + SoD avanzado + workflow largo | v2 | autorizacion_aprobaciones |
| Batches + PaymentProvider | v2 | liquidacion_settlements §4-5 |
| Disputes: workflow + clawback offset automático | v2 | liquidacion_settlements §7-8 |
| Matching AI de statements (recon. avanzada) | v2 | negocio §6 |
| Simulador what-if histórico | v2-v3 | jerarquia_reglas §10 |
| Módulo fiscal multi-jurisdicción | v3 | impuestos §2-13 |
| Crypto-shredding + PII vault | v3 | proteccion_datos §6-7 |
| Connectores REST/DB/CDC genéricos | v3 | stack_arquitectura §10-11 |
| Kafka adapter | v3 | stack_arquitectura §13 |
| Temporal para workflows largos | v3 | stack_arquitectura §24 |
| Kubernetes + Helm enterprise | v3 | stack_arquitectura §17-18 |
| AI layer (contrato→reglas, copilot) | v3 | negocio §10-13, arquitectura §26-27 |
| SDKs multi-lenguaje | v3 | stack_arquitectura §8 |

---

# 8. Modelo de negocio y go-to-market

## Capas de ingresos

Las tres capas son concreciones comerciales de los **dos modos de consumo** (ver §Definición · "Dos modos de consumo"): SaaS es Modo A puro; self-hosted y embed son Modo B (componente en su infra o dentro de su producto).

| Modelo | Modo | Para quién | Pricing |
|---|---|---|---|
| **SaaS suscripción** | A · Plataforma | Agencias y redes | Tarifa por tamaño de red (# agentes activos) + volumen de ventas calculadas |
| **Licencia self-hosted** | B · Componente en su infra | Grandes consorcios con requisito on-premise | Licencia anual + soporte |
| **Embed / white-label** | B · Componente en su producto | Fabricantes de software travel | Licencia por instancia o rev-share; un deal = cientos de agencias |

## Ancla de precio

> No competir contra "software": competir contra lo que reemplaza.

Horas de back-office mensual, disputas entre agentes y ventas con
comisión mal calculada. Si un consorcio mueve millones al año en
comisiones vía Excel, la suscripción es un redondeo en su contabilidad.

El dolor económico está documentado: casos públicos de automatización de procesos de comisión reportan reducciones importantes de costes operativos y mejoras en recuperación de comisiones (p. ej., el caso publicado por Onyx). Son cifras del propio proveedor — tomarlas como tales — pero confirman que las empresas asignan presupuesto real a este problema.

## Un producto, pero no dos movimientos comerciales el día uno

El motor es API-first y sirve dos negocios (SaaS para agencias, API para fabricantes de software), pero **comercialmente son ventas distintas**: una agencia quiere *"quítame el Excel y dime quién cobra qué"*; un fabricante pregunta por SLA, webhooks, versionado, idempotencia, throughput, sandbox, tenancy y seguridad; un enterprise además exige SSO, RBAC, auditoría, approvals y data residency. Atacar los dos frentes a la vez desde el día uno multiplica el riesgo.

```text
Construir:  API-first (el motor sirve para embeber después)
Vender:     UNA sola experiencia primero → agencias / host / consorcios
Luego:      convertir el motor en infraestructura embebible (Modo B)
```

Ese "luego" tiene un orden concreto. No se lanzan las tres modalidades a la vez; se abren por fases, y cada fase mapea a una banda de versión:

```text
Fase 1  Engine + Commission Cloud encima                          v1
        (vendes la plataforma a agencias)
Fase 2  Tu propia suite consume el engine por una API interna      v1 → v1.5
        estable (dogfooding: te tratas como otro cliente)
Fase 3  Abres esa API a 2–3 clientes piloto                        v2
Fase 4  API como producto formal: docs, webhooks, sandbox, SLA     v2
Fase 5  Private deployment / on-prem si el mercado lo pide         v2 → v3
```

Así no se especula con una plataforma developer desde el día uno: la API se endurece consumiéndola tú primero, y solo se abre a terceros cuando ya está probada.

## El ICP: no "una agencia", sino una con complejidad de comisión

"Agencia de viajes" es demasiado amplio: una con 3 agentes dirá *"tengo un Excel y me funciona"*. El dolor grande aparece con volumen y combinaciones. El ICP inicial:

> **Host agencies, redes o consorcios con ~20–500 agentes y múltiples acuerdos de comisión con proveedores.**

Y el indicador de dolor no es el headcount, es la **complejidad de comisión**:

```text
commission complexity ≈
   nº reservas × nº proveedores × nº esquemas de comisión × nº agentes
```

Cuantas más combinaciones, más insostenible el Excel y más valor entrega el motor. Es la métrica que cualifica un lead mejor que "tamaño de empresa".

## A quién venderle primero

```text
1. Host agencies y consorcios          dolor máximo: split host/advisor,
                                       miles de asesores, pagador central
2. Back office / Finance de la red     comprador de la reconciliación:
                                       "¿cuánto nos falta cobrar y a quién?"
3. Agencias empaquetadoras medianas    el flujo paquete es el demo estrella
4. Fabricantes de software travel      canal multiplicador vía embed (fase 2)
```

El diferenciador comercial no es la calculadora, es la frase: *from supplier contract to advisor payout, every commission explained*. Pero el **comprador económico** es Finance / COO / dirección de la red, no el agente: al agente se le muestra *"esta venta te deja 81 €"*; al comprador se le vende el source of truth de lo que cada proveedor debía y de lo que falta por cobrar — *"sé cuánto debo cobrar, cuánto debo pagar y por qué"*.

## El pitch de 10 minutos

```text
1. "Suban su Excel actual"          importación en vivo
2. Crear 3 reglas frente a ellos    dos minutos, sin desarrolladores
3. Abrir el catálogo COMO AGENTE    "esto tuyo te deja €81"
4. Registrar una venta              saldo sube, trace completo
5. Cancelarla                       saldo baja solo
6. Pulsar "¿por qué?"               fin de las disputas de fin de mes
```

El cierre emocional es siempre el mismo:

> ¿Cuántas horas pasó su gente este mes discutiendo comisiones por WhatsApp?

## Métricas de valor para el contrato

```text
Horas de back-office ahorradas / mes
Disputas de comisión: de N a ~0
Productos vendidos con comisión €0 por error: atrapados por el guardián
Tiempo de alta de producto nuevo: días → minutos
```

## Secuencia estratégica

```text
v1           Motor de la cadena: contract → override → gross →
             split host/advisor → tiered básico → neto explicado
             + conciliación básica CSV: el gancho de Finance ya en el MVP
v1 Enterprise RBAC + approvals: desbloquea el enterprise
v1.5         Conciliación rica (import API + disputes + drill-down):
             profundiza el gancho de Finance / back office
v2           Matching AI, pagos automáticos, agregación persistente,
             ledger doble partida                          retención
v3           Segunda vertical (seguros o inmobiliaria: ya tienen clawback)
             + marketplace de plantillas de reglas
```

---

# 9. Resumen

```text
                CLIENTE (agencia / consorcio)
                        │
     1. Sube sus productos al catálogo
     2. Crea reglas una vez, las aplica y reutiliza
                        │
                        ▼
        ┌───────────────────────────────┐
        │         MOTOR v1              │
        │                               │
        │  Catálogo × Reglas            │
        │  (jerarquía + CEL + versión)  │
        │        │                      │
        │        ▼                      │
        │  Basis → Contrato → Gross →   │
        │  Split → Neto del agente      │
        │  ANTES de vender, trace       │
        │  por nodo                     │
        │        │                      │
        │        ▼                      │
        │  Venta registrada → cálculo   │
        │  confirmado → movimiento      │
        │  append-only → saldo          │
        └───────────────┬───────────────┘
                        ▼
          El agente consulta API/UI:
          catálogo con SU comisión
                        │
                        ▼
        El cliente liquida con sus medios
```

Principios finales:

> Configuras una vez; cada agente ve cuánto gana antes de vender.

> Una venta genera su cadena económica completa: contrato de proveedor, override, gross de agencia, split host/advisor y neto; cada euro con su nodo y su trace.

> Un paquete vale la suma explicada de sus partes.

> Una cancelación revierte saldo; jamás borra historia.

> No solo decimos cuánto se debía: ya en v1 (conciliación básica CSV) cruzamos lo que se debía contra lo que entró y señalamos lo que falta; v1.5 lo profundiza.

> Commission infrastructure built for travel distribution: from supplier contract to advisor payout, every commission explained.

> Un mismo motor, dos formas de venderlo: plataforma cloud donde configuran las reglas, o componente embebido donde ya trabajan. El binario no cambia; cambia quién pone la UI y dónde corre.

> Lo estructural el día uno; la maquinaria, cuando el volumen la pida.

> Cada componente de los documentos existe con versión asignada: nada se inventa después, todo se agenda.
