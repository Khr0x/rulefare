# Travel Commission Engine
### Resumen ejecutivo · MVP1

> **La infraestructura de comisiones para la distribución de viajes.**
> Calcula, explica y concilia quién cobra cuánto en cada venta —y encuentra el dinero que se escapa.

## 1. Síntesis

**Travel Commission Engine** es la capa de software que calcula, explica y controla **quién cobra cuánto** en cada venta de viaje: desde la comisión que el proveedor paga a la agencia hasta el importe que corresponde a cada agente —y de cada peso sabe decir **por qué**.

Hoy esa lógica vive en hojas de Excel y mensajes de WhatsApp, con un coste real en horas de administración, disputas internas y comisiones que el proveedor debía pagar y **nunca se llegan a cobrar**. El motor sustituye ese proceso por un cálculo automático, trazable y específico del sector viajes, y da un paso más: cruza lo que **se debía cobrar** contra lo que **realmente entró** y señala lo que falta. El comprador no paga por ahorrar trabajo administrativo; paga porque **le encontramos dinero**.

El MVP se dirige primero a **host agencies, redes y consorcios de ~20 a 500 agentes** con múltiples acuerdos de comisión, y se ofrece como plataforma en la nube. La ventaja no es una función aislada: se acumula (datos, reglas del sector, histórico de contratos, conciliación) hasta convertirse en una capa de inteligencia financiera para la distribución de viajes.

## 2. El problema y la oportunidad

En una agencia de viajes, la comisión casi nunca es un simple "precio × porcentaje". En una misma venta cobran varias partes:

- El **proveedor** (hotel, aerolínea, mayorista) paga una comisión a la agencia.
- La **agencia** —o su red / consorcio— retiene una parte y reparte el resto.
- El **agente** que cerró la venta cobra su porcentaje.

Y cada porcentaje cambia según el proveedor, las fechas, el destino, el tipo de tarifa, los impuestos o componentes que no dan comisión, los bonos por volumen o las cancelaciones.

Gestionado en Excel y WhatsApp, esto genera un coste recurrente y silencioso:

- Horas de trabajo administrativo cada mes.
- Disputas sobre quién cobra qué.
- Ventas cerradas con la comisión mal calculada.
- Comisiones del proveedor que se pierden por no reclamarse a tiempo.

Es un problema con presupuesto asignado: los casos públicos de automatización de comisiones reportan reducciones relevantes de coste operativo y mejoras en la recuperación de comisiones. La oportunidad es capturar ese gasto con una herramienta especializada.

## 3. La solución

Se le proporcionan al sistema los productos, las ventas y las condiciones; el motor aplica las reglas y devuelve el **reparto completo, paso a paso y explicado**:

```text
Venta → parte comisionable → contrato del proveedor → comisión de la agencia
      → reparto con el agente → bonos → comisión final del agente
```

Dos momentos importan:

- **Antes de vender**, el agente sabe cuánto gana ("esta venta te deja $1,600 MXN").
- **Después de vender**, finanzas puede auditar cada cifra, viendo exactamente qué regla y qué contrato la produjeron.

Bajo el capó son tres piezas encadenadas: **reglas** (se definen una vez y se reutilizan), **cálculo** (recorre toda la cadena) y **explicación** (deja el "por qué" de cada tramo). Las dos primeras las tiene cualquier calculadora; la explicación paso a paso y el estar diseñado **específicamente para viajes** son lo que lo convierte en infraestructura y no en una herramienta de cuentas.

**Adopción sin fricción.** Un cliente con cientos de contratos no debería teclearlos a mano —eso solo trasladaría su Excel a otra pantalla. Por eso el MVP incluye un **importador**: sube su Excel de contratos y tarifas, el sistema propone las reglas en borrador, el usuario las revisa y publica. La promesa "sube tu Excel de siempre" es un flujo real, no un eslogan.

## 4. Propuesta de valor: de "ahorrar trabajo" a "encontrar dinero"

Explicar cada peso es valioso, pero por sí solo no cierra un contrato grande. Lo que mueve al comprador es la suma completa —y sobre todo lo último:

> **Cuánto debería cobrar + cuánto tengo que pagar + cuánto dinero me falta.**

En pantalla se traduce en un control de caja de comisiones, no en una calculadora:

```text
Comisiones esperadas este mes    $3,700,000 MXN
Cobradas                         $3,400,000 MXN
Pendientes                         $300,000 MXN
23 reservas requieren revisión
```

El mensaje se adapta a cada interlocutor. Al **agente** se le muestra *"esta venta te deja $1,600 MXN"*. Al **comprador económico** —dirección o finanzas de la agencia, no el agente— se le ofrece algo más fuerte: *"sé exactamente cuánto debo cobrar, cuánto debo pagar y por qué"*.

## 5. Mercado objetivo

El motor sirve a muchos perfiles (agencias, redes, consorcios, fabricantes de software, grandes empresas), pero no conviene venderles a todos a la vez: cada uno es una venta distinta. La estrategia inicial concentra el esfuerzo en **una sola experiencia**: la plataforma en la nube para agencias, redes y consorcios; la integración en software de terceros llega después.

El cliente ideal no es "cualquier agencia" —una de tres personas se apaña con Excel—. El dolor grande aparece con la **complejidad de comisión**, es decir, cuando se multiplican reservas, proveedores, esquemas de comisión y agentes:

> **Perfil objetivo: host agencies, redes o consorcios de ~20 a 500 agentes con varios acuerdos de comisión.**

Ese indicador cualifica un cliente mejor que su tamaño de plantilla.

## 6. Ventaja competitiva y defensibilidad

No competimos con las herramientas genéricas de comisiones (Spiff, Varicent): modelamos la **realidad concreta del sector viajes** que ellas no cubren —el contrato con el proveedor, los componentes que no dan comisión, el reparto entre red y agente, y el control de lo que realmente se cobra.

Y la ventaja no es una función copiable: se **acumula**. Modelo de datos de viajes, biblioteca de reglas del sector, histórico de contratos, integraciones, matching de liquidaciones y datos sobre cómo paga cada proveedor. Con el tiempo, esa acumulación abre una capa nueva —una **inteligencia financiera** del sector— capaz de afirmar lo que ninguna calculadora puede:

> *"Este proveedor suele liquidar a 47 días; tienes $760,000 MXN fuera de plazo."*

En ese punto el producto deja de ser una calculadora de comisiones y pasa a ser el sistema que sabe, por proveedor, qué se debe y cuándo llega. El MVP no construye esa predicción, pero sí coloca los cimientos que la hacen posible: contratos con fechas, conciliación con fechas e historial inmutable.

Además, es la **primera pieza** de un ecosistema mayor (CRM, catálogo de productos, generador de presupuestos, conciliación, copiloto) que puede apoyarse en este motor sin reprogramar la lógica de comisiones.

## 7. Alcance del MVP y hoja de ruta

En esta primera versión las reglas son sencillas e intermedias (porcentaje fijo, importe fijo, por tramos, reparto agencia/agente, versionado). Lo más avanzado —repartos a muchas partes, acumulados, descuentos por volumen— queda para versiones posteriores.

| Fase | Qué incluye | Por qué ahora |
|---|---|---|
| **v1** | Cálculo completo de la comisión, importador de reglas desde Excel, reglas reutilizables, vista previa antes de vender, saldos que se ajustan solos al cancelar una reserva, separación por cliente y **conciliación básica** (subir el CSV del proveedor y ver, por reserva, qué está cobrado / falta / con diferencia) | El motor y su gancho de dinero: no solo explica cada peso, también dice **cuánto falta por cobrar** |
| **v1 Enterprise** | Permisos por rol y aprobaciones (que una persona configure y otra valide) | Sin control de quién puede hacer qué, no se cierra un cliente grande |
| **v1.5** | Conciliación **rica**: entrada de extractos por conexión/API (no solo CSV manual), gestión de desajustes con estados (abierto / en revisión / resuelto) y detalle peso a peso | Profundiza el gancho para finanzas: *"cuánto falta por cobrar y a quién"* |

**Fuera de alcance por ahora (v2 y posteriores):** lectura automática de extractos con IA, pagos automáticos, contabilidad de doble partida, cambio de divisa y conexiones con sistemas externos (GDS/ERP).

## 8. Modelo de comercialización

El motor es uno solo; cambia únicamente la forma de consumirlo. Es un **único core, no tres motores distintos**, lo que evita mantener la misma lógica cinco veces.

- **Plataforma en la nube** *(en el MVP)*: el cliente entra a la aplicación web, configura productos y reglas, registra ventas y consulta comisiones. No integra nada. Ideal para agencias, redes y consorcios.
- **Componente integrado** *(v2)*: el motor se conecta al software que el cliente ya usa (su motor de reservas, su back office o el producto de un fabricante), en nuestra nube o en su propia infraestructura. La API como producto formal —con documentación, sandbox y SLA— y la instancia dedicada/on-premise son fase posterior.
- **Nuestra propia plataforma** *(consumidor interno)*: la suite del ecosistema usa el mismo motor por dentro —"powered by Travel Commission Engine"— en lugar de reprogramar la lógica. Esto valida el motor con nuestro propio producto.

No se lanzan las tres modalidades a la vez. El orden reduce el riesgo:

```text
Fase 1  Motor + plataforma en la nube para agencias                v1
Fase 2  Nuestra suite consume el motor por una API interna estable  v1 → v1.5
Fase 3  Apertura de la API a 2–3 clientes piloto                    v2
Fase 4  API como producto formal (docs, webhooks, sandbox, SLA)     v2
Fase 5  Despliegue privado / on-premise si el mercado lo pide       v2 → v3
```

Un cliente puede empezar en la plataforma y migrar a la integración más adelante sin cambiar de motor ni reconfigurar reglas.

## 9. Beneficios esperados

Las métricas con las que se defiende el contrato:

- Horas de back-office ahorradas al mes.
- Disputas de comisión de fin de mes: de muchas a casi ninguna.
- Ventas con comisión de $0 detectadas antes de cerrarse.
- Alta de un producto nuevo: de días a minutos.
- Comisiones del proveedor sin cobrar: **detectadas**.

## 10. Validación: demostración de 10 minutos

El valor se prueba en una sesión corta con datos del propio cliente:

1. El cliente sube su Excel de siempre y el importador propone las reglas.
2. Se revisan y publican tres reglas en directo.
3. Se abre el catálogo como agente: *"esta venta te deja $1,600 MXN"*.
4. Se registra una venta y el saldo sube.
5. Se cancela y el saldo baja solo.
6. Se pulsa **"¿por qué?"** y aparece el detalle de cada peso.
7. Se sube el CSV del proveedor y aparece el remate: *"esto está cobrado, esto falta"*.

## 11. Conclusión y próximos pasos

El MVP entrega un motor de comisiones travel-native que **explica cada peso y encuentra el dinero que se escapa**, dirigido a un segmento con dolor demostrable y con una vía de crecimiento clara —de plataforma a infraestructura, y de calculadora a inteligencia financiera del sector.

Próximo paso: validar el alcance de la v1 con dos o tres agencias del perfil objetivo y arrancar la construcción del motor junto a la plataforma en la nube (Fase 1).
