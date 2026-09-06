# Travel Commission Engine — Consistencia de eventos y concurrencia

Documento complementario a:

> `travel_commission_engine_stack_arquitectura.md`

El documento de arquitectura prescribe PostgreSQL como fuente de verdad y NATS JetStream como bus de eventos, pero no explica cómo se garantiza que ambos sistemas permanezcan coherentes. En una plataforma financiera, un evento perdido o fantasma no es un bug menor: es dinero mal calculado o duplicado.

---

## El problema: escritura dual

El flujo ingenuo:

```text
1. INSERT booking en PostgreSQL
2. PUBLISH booking.created a NATS
```

Tiene dos fallos estructurales:

```text
Si el proceso muere entre 1 y 2:
  el estado existe pero nadie lo sabe → EVENTO PERDIDO
  (una reserva sin comisión calculada, para siempre)

Si se invierte el orden (publicar primero):
  el evento anuncia algo que puede no existir → EVENTO FANTASMA
  (cálculos sobre reservas inexistentes)
```

A esto se suma la concurrencia: ajustes simultáneos sobre la misma reserva y saldos materializados actualizados en paralelo pueden corromper el ledger si no hay disciplina de orden y exclusión.

---

# 1. Principio fundamental

> El estado y el evento nacen en la misma transacción de PostgreSQL. Publicar es un efecto posterior y recuperable.

Y la segunda mitad:

> La entrega será at-least-once; los efectos serán exactly-once mediante idempotencia.

No se persigue exactly-once en el transporte (irrealista); se construye en los consumidores.

---

# 2. Transactional Outbox

## Tabla

```sql
outbox
---------------------------
id                UUID
aggregate_type    TEXT    -- booking | commission | settlement ...
aggregate_id      TEXT
event_type        TEXT    -- booking.created | ledger.entry.created ...
payload           JSONB
headers           JSONB   -- tenant, schema_version, trace context
nats_msg_id       TEXT UNIQUE
created_at
published_at      TIMESTAMPTZ NULL
attempts          INT DEFAULT 0
```

## Escritura

```text
TX ÚNICA EN POSTGRESQL:

    INSERT booking ...
    INSERT ledger_transaction + entries ...
    INSERT calculation_result ...
    INSERT outbox (booking.created, payload, headers)

COMMIT
```

El evento hereda la atomicidad de la transacción financiera. Si el commit ocurre, el evento existe. Si no ocurre, ni estado ni evento existen.

## Relay

Proceso Go separado del API:

```text
bucle:

  1. CLAIM
     SELECT ... FROM outbox
     WHERE published_at IS NULL
       AND shard = :mi_shard
     ORDER BY created_at
     LIMIT 100
     FOR UPDATE SKIP LOCKED

  2. PUBLICAR cada evento a JetStream
     header Nats-Msg-Id = nats_msg_id

  3. MARCAR published_at = now()
```

Detalles que importan:

```text
✓ SKIP LOCKED permite varios relays sin bloquearse
✓ El sharding del claim preserva el orden por agregado (§4)
✓ Los headers llevan trace context OTel:
  el evento conserva el trace del request que lo originó
✓ Purga de publicados por retención (la tabla nunca crece sin límite)
```

Descarte deliberado: Debezium outbox event router como mecanismo primario. Ya operamos NATS; un relay propio de ~200 líneas es más simple de operar self-hosted y auditar. Queda documentado como alternativa enterprise si existiera ya Debezium por CDC.

---

# 3. Matriz de modos de fallo

| Fallo | Resultado | Por qué |
|---|---|---|
| Crash antes de COMMIT | Nada ocurrió | Ni estado ni evento; TX atómica |
| Crash tras COMMIT, antes de publicar | Relay reintenta | El evento vive en outbox |
| Publica, muere antes de marcar | Redelivery absorbido | Dedup JetStream por `Nats-Msg-Id` |
| NATS caído | Acumulación visible | Outbox crece = backpressure honesto |
| Consumidor procesa dos veces | Efecto único | Idempotencia por `event_id` |

Propiedad clave de la fila de NATS caído:

> La pérdida silenciosa es imposible por construcción. Si NATS no acepta, el outbox crece y dispara alerta.

---

# 4. Orden por agregado

Dos mutaciones casi simultáneas de la misma reserva (enmienda + cancelación) no deben procesarse en orden arbitrario.

## Particionado determinístico

```text
subject: booking.{org_id}.{booking_id}

worker_i es dueño de los shards donde
hash(booking_id) % N == i
```

```text
✓ La misma reserva siempre la procesa el mismo worker
  → secuencial por agregado
✓ Workers distintos procesan agregados distintos
  → paralelismo global
✓ Rebalanceo solo mueve agregados completos, nunca a mitad
```

## Versión optimista de agregado

Red de seguridad independiente del sharding:

```sql
UPDATE booking
SET status = $new, version = version + 1
WHERE id = $id AND version = $expected
```

```text
0 filas afectadas → conflicto de concurrencia
  → la operación se reencola, jamás se descarta
```

Todo evento portará:

```json
{
  "aggregate_id": "...",
  "aggregate_version": 7,
  "event_id": "..."
}
```

Si un consumidor recibe versión 8 antes que la 7, retiene y reordena dentro de una ventana pequeña; fuera de ventana, `nak` con delay y reintento. Las versiones son monotónicas por construcción de la base de datos.

---

# 5. Concurrencia financiera

## Saldos: jamás read-modify-write

```text
✗ INCORRECTO
   leer saldo → calcular nuevo → escribir saldo
   dos procesos simultáneos pierden una actualización

✓ CORRECTO
   INSERT entrada + UPSERT delta en la MISMA TX del asiento:

   INSERT INTO ledger_entry (...) VALUES (...);

   INSERT INTO account_balance(account_id, period_key, delta)
   VALUES ($acc, $period, $delta)
   ON CONFLICT (account_id, period_key)
   DO UPDATE SET
     closing_balance = account_balance.closing_balance
                      + EXCLUDED.delta;
```

El UPSERT serializa sobre la fila de la cuenta automáticamente. Ventaja del modular monolith: PostgreSQL es el único punto de coordinación necesario. Sin locks distribuidos en la v1.

## Transiciones de estado protegidas

Cancelación vs payout en carrera:

```sql
UPDATE commission
SET status = 'PAYABLE'
WHERE id = $1 AND status = 'CONFIRMED'
```

Si otra rama ya movió el estado, la condición falla y el flujo perdedor aborta limpiamente. Ningún estado cambia sin verificar su predecesor bajo la misma fila.

---

# 6. Idempotencia en consumidores

Cada consumidor registra lo procesado:

```sql
processed_event
---------------------------
event_id      UUID PRIMARY KEY
consumer      TEXT
processed_at  TIMESTAMPTZ
```

Patrón obligatorio:

```text
BEGIN;
  INSERT INTO processed_event ... ON CONFLICT DO NOTHING;

  IF insertada:
      ejecutar efectos (idempotentes por clave de negocio)
  ELSE:
      saltar; ya procesado
COMMIT;
```

Las claves de idempotencia de negocio ya existen en los documentos previos:

```text
Ledger      idempotency_key derivada del cálculo
Webhooks    idempotency_key en el envelope del evento
Agregación  contribución referenciando source_calculation_id
```

---

# 7. Exactly-once efectivo

```text
        OUTBOX transaccional
                 │
                 ▼
     entrega AT-LEAST-ONCE (transporte)
                 │
     ┌───────────┼─────────────┐
     ▼           ▼             ▼
 dedup JetStream  idempotencia  claves de negocio
 (Nats-Msg-Id)    (event_id)   (ledger, webhooks)
     └───────────┼─────────────┘
                 ▼
      EFECTOS EXACTLY-ONCE
   cada asiento ocurre una sola vez
```

Esta tríada es la que se declara ante auditoría. No vendemos "no perdemos mensajes"; demostramos "un reproceso completo produce los mismos asientos".

---

# 8. Operación

```yaml
events:

  provider: nats

  outbox:

    relay_shards: 4

    poll_batch: 100

    max_attempts_before_alert: 20

    purge_after: 30d

  alerts:

    outbox_lag_seconds: 60
    # crecimiento sostenido del outbox no publicado

    dlq_depth: 1
    # cualquier mensaje en DLQ pagina a humans
```

Métricas esenciales vía OpenTelemetry:

```text
outbox_unpublished_count
outbox_oldest_age_seconds
relay_publish_duration
consumer_lag_by_subject
dlq_total
```

---

# 9. Qué NO construir

```text
✗ Publicar eventos desde el proceso de negocio
✗ Dos fases manuales (escribir, luego "recordar publicar")
✗ Exactly-once en el transporte como objetivo
✗ Locks distribuidos externos (Redis) siendo monolito modular
  — PostgreSQL basta hasta que haya múltiples bases
✗ Reordenamiento infinito: ventana acotada + DLQ
```

---

# 10. Fases

Qué entra en cada versión de este módulo está clasificado en:

> `../mvp/travel_commission_engine_alcance_v1.md` (sección 7)

Resumen: en v1 cálculo síncrono por API y tabla de jobs con
idempotencia; outbox, NATS y orden por agregado en v2;
Kafka adapter y replay histórico en v3.

---

# 11. Impacto en el resto de la arquitectura

```text
§12   Event Bus           NATS recibe eventos que nacen atómicos
§13   Kafka adapter       mismos contratos de outbox/dedup/idempotencia
§16   Webhooks            emisor también consume del outbox;
                          idempotency_key ya definida ahí
Ledger doble partida      idempotency_key del asiento = event lineage
Agregación                contribuciones en la TX del cálculo;
                          orden por agregado protege acumuladores
Autorización              eventos de aprobación pasan por el mismo canal
§25   Observabilidad      trace context viaja en headers del outbox
```

---

# 12. Resumen

```text
   PROCESO DE NEGOCIO
          │
          ▼
   ┌─────────────────────────────┐
   │ UNA TRANSACCIÓN POSTGRES    │
   │                             │
   │ estado + ledger + outbox    │
   └──────────────┬──────────────┘
                  │ COMMIT
                  ▼
        ┌──────────────────┐
        │ OUTBOX RELAY     │  shards por hash(aggregate_id)
        │ SKIP LOCKED      │  orden garantizado por agregado
        └────────┬─────────┘
                 │ at-least-once
                 ▼
          NATS JETSTREAM
       dedup por Nats-Msg-Id
                 │
                 ▼
        CONSUMIDORES IDEMPOTENTES
       event_id + claves de negocio
                 │
                 ▼
        EFECTOS EXACTLY-ONCE
```

Principios finales:

> Estado y evento comparten transacción o no existen.

> Reentregar es barato; aplicar dos veces es imperdonable.

> El mismo agregado, el mismo camino. Agregados distintos, caminos paralelos.

> El outbox creciendo es información, no ruido.
