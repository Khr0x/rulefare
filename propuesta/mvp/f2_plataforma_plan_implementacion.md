# F2 · Plan de implementación de la plataforma multi-tenant

> Monolito modular Go + PostgreSQL alrededor del motor, sin tocar `engine`

| Campo | Valor |
|---|---|
| Versión | 0.2 |
| Última actualización | 2026-09-26 |
| Estado | 🟡 En curso · T1.1 y T1.2 implementadas con pruebas locales |
| Inicio objetivo | 2026-09-28 |
| Fin objetivo | 2026-10-16 |
| Duración | 3 semanas |
| Responsable | Backend + Frontend/Full-stack |
| Dependencias técnicas | F1 ✅ |
| Dependencia de negocio | Ninguna: F0 diferida por [decisión del 2026-09-26](./roadmap.md#decisión-del-2026-09-26-f2-avanza-con-f0-abierta) |

Este plan desarrolla la [F2 del roadmap](./roadmap.md#f2--fundación-de-plataforma-y-multi-tenant). La arquitectura parte de [Arquitectura propuesta del MVP](./roadmap.md#arquitectura-propuesta-del-mvp); multi-tenancy, seguridad y stack salen de [Alcance v1](./travel_commission_engine_alcance_v1.md#bloque-5--multi-tenant).

## 1. Objetivo

Entregar la base sobre la que se construyen F3–F8:

1. `rulefare serve` levanta API HTTP, UI React embebida y conexión a PostgreSQL en el mismo binario de F1.
2. Un solo comando (`docker compose up`) levanta todo localmente.
3. Toda entidad de negocio pertenece a una organización y el aislamiento se prueba automáticamente.
4. El dinero tiene un tipo único, exacto y con redondeo explícito.
5. API, UI y base de datos exponen salud; los logs son estructurados y correlacionables.

F2 **no** incorpora dominio travel, reglas persistidas ni cálculo. Tampoco usa `engine` en tiempo de ejecución: solo garantiza que la plataforma pueda importarlo sin que el motor dependa de ella.

## 2. Entregables

- Subcomando `rulefare serve` y subcomandos de bootstrap `rulefare orgs create` y `rulefare apikeys create|revoke`.
- Paquetes `internal/platform` (config, logging, salud), `internal/postgres` (pool, migraciones, transacciones), `internal/tenancy` (organizaciones y API keys), `internal/money` e `internal/httpapi`.
- Migraciones SQL versionadas en `migrations/`, embebidas en el binario.
- Contrato `api/openapi.json` validado en CI y sincronizado con las rutas registradas.
- Aplicación `web/` (Vite + React + TypeScript) compilada y embebida con `go:embed`.
- `Dockerfile` multi-etapa y `compose.yaml` con `app` y `postgres`.
- CI ampliada: PostgreSQL real, build del frontend, humo de Docker Compose y pruebas de arquitectura.
- Evidencia de cierre en `propuesta/mvp/verificacion/<fecha>/`.

## 3. Alcance funcional

### Incluido

- Configuración por variables de entorno con validación al arrancar y valores por defecto seguros.
- Logs JSON con `log/slog`, `request_id` y propagación de `traceparent` (W3C).
- `GET /healthz` (proceso vivo) y `GET /readyz` (PostgreSQL alcanzable y migraciones al día).
- Migraciones hacia adelante, idempotentes y serializadas con advisory lock.
- Tablas `organization` y `api_key`; `organization` fija la divisa operativa única de v1.
- Autenticación por API key; `organization_id` sale siempre de la credencial.
- Endpoints mínimos: `GET /v1/organization`, `GET /v1/api-keys`, `DELETE /v1/api-keys/{id}`.
- Errores HTTP con `application/problem+json` y códigos estables.
- `money.Money` sin `float`, con redondeo `HALF_UP`.
- Tipo `OpaqueRef` para `customer_ref` y detector de posible PII en metadata en modo advertencia.
- UI mínima: página que consulta `/readyz` y muestra versión y estado.

### Fuera de F2

- Catálogo, proveedores, contratos, bookings y adaptador travel → F3.
- Persistencia y publicación de rulesets, caché de `Program` por organización y versión → F4.
- Política ante `CONDITION_ERROR`, aritmética de outcomes, `SIMULATE`/`COMMIT` → F5.
- Usuarios, login de la UI, roles y audit log de reglas → F4/F7 (el rol por usuario sigue siendo estructural desde v1, pero no hay usuarios que modelar todavía).
- Exportador OpenTelemetry, configuración YAML y réplicas → cuando una fase lo exija.
- Row-Level Security de PostgreSQL → evaluar en F9 como defensa adicional.

## 4. Arquitectura

```text
cmd/rulefare
├── rules validate|evaluate   → engine            (sin cambios; no abre BD ni HTTP)
├── serve                     → internal/httpapi → internal/tenancy → internal/postgres
│                                               → web (assets embebidos)
└── orgs / apikeys            → internal/tenancy → internal/postgres
```

### Regla de dependencias

```text
engine            → Go stdlib + CEL                       (sin cambios desde F1)
internal/money    → Go stdlib
internal/platform → Go stdlib
internal/postgres → pgx
internal/tenancy  → internal/postgres
internal/httpapi  → internal/tenancy, internal/money, internal/platform
cmd/rulefare      → todo lo anterior

engine ✗ internal/*          engine ✗ pgx / net/http
internal/money ✗ internal/*  (dependencia hoja, reutilizable por F5)
```

La regla se verifica con una prueba Go (`architecture_test.go`) que sustituye al script de CI actual y también corre con `go test ./...` local.

### Estructura de archivos objetivo

```text
api/openapi.json
cmd/rulefare/            # main.go ampliado: serve, orgs, apikeys
internal/
├── httpapi/             # router net/http, middleware, handlers, problem+json
├── money/               # Money, HALF_UP, parseo decimal
├── platform/            # config, slog, request_id, salud
├── postgres/            # pool pgx, migrador, WithTx
└── tenancy/             # organizaciones, API keys, OpaqueRef, detector PII
migrations/              # 0001_organization.sql, 0002_api_key.sql, ...
web/                     # Vite + React + TS; dist/ embebido
architecture_test.go
Dockerfile
compose.yaml
```

No se crean paquetes sin código real: `catalog`, `rules`, `calculation`, `ledger`, `reconciliation` y `jobs` llegan con sus fases.

## 5. Decisiones técnicas

| Tema | Decisión | Motivo | Alternativa descartada |
|---|---|---|---|
| HTTP | `net/http` con `ServeMux` de Go ≥ 1.22 (método + parámetros) | Cubre el MVP sin dependencias | chi, gin, echo |
| PostgreSQL | `pgx/v5` + `pgxpool`, PostgreSQL 18 | Driver de referencia; tipos nativos y `NUMERIC` exacto | `database/sql` + lib/pq |
| Migraciones | Runner propio (~100 líneas): SQL embebido, tabla `schema_migrations`, advisory lock, solo hacia adelante | Solo hace falta aplicar SQL en orden; sin dependencia | goose, golang-migrate |
| SQL | SQL escrito a mano en los repositorios | Pocas consultas; el `organization_id` queda visible en cada una | ORM, sqlc (reconsiderar en F3 si crece) |
| Dinero | `Money{Minor int64, Currency string}` en unidades menores; operaciones con tasas vía `math/big.Rat` y redondeo `HALF_UP` a unidades menores | Exacto, sin dependencias; coincide con los centavos del Excel de F0 | `shopspring/decimal`, `float64` |
| Dinero en BD | `BIGINT amount_minor` + `CHAR(3) currency`; `NUMERIC` para tasas | Sin conversión con pérdida | `NUMERIC` para importes, `REAL/DOUBLE` |
| Divisas | Tabla embebida ISO 4217 con exponente (MXN, USD, EUR, …); una divisa por organización | Decisión v1 de F0 (supuesto no validado) | Multi-divisa por organización |
| API keys | Formato `rf_<prefijo8>_<secreto>`; se guarda prefijo + SHA-256 del secreto; comparación en tiempo constante; se muestra una sola vez | Estándar; el secreto nunca se persiste en claro | bcrypt (innecesario con 256 bits de entropía) |
| Bootstrap | Organizaciones y claves se crean por CLI contra la BD | No existe aún un administrador ni login | Endpoint admin sin autenticar |
| Contrato API | `api/openapi.json` escrito a mano, lint con Redocly en CI y prueba de paridad ruta ↔ spec | El contrato es la fuente de verdad; JSON se lee con la stdlib | Generación desde código, oapi-codegen |
| Errores | `application/problem+json` con `code` estable | Estándar RFC 9457 | Formato propio |
| Config | Solo variables de entorno `RULEFARE_*` | Suficiente en F2; YAML se añade si una fase lo necesita | YAML desde el inicio |
| Observabilidad | `log/slog` JSON, `request_id`, `traceparent` propagado y registrado | Correlación sin SDK pesado | SDK OpenTelemetry completo (diferido) |
| Frontend | Vite + React + TypeScript; Node fijado en `.nvmrc`; `web/dist` embebido con `//go:embed all:dist` y un placeholder versionado | `go build` funciona sin Node; la imagen Docker compila la UI | Next.js, servir la UI aparte |
| Despliegue | `Dockerfile` multi-etapa (node → go → distroless) y `compose.yaml` con healthchecks | “Un comando” real y reproducible | Kubernetes, Helm |

### Aislamiento multi-tenant

1. El middleware de autenticación resuelve la API key y coloca un `tenancy.OrgID` en el `context.Context`.
2. Los repositorios reciben `OrgID` como parámetro obligatorio y lo incluyen en cada `WHERE` e `INSERT`; no existe función de repositorio sin él.
3. Los handlers nunca leen `organization_id` del body, query ni path; si llega, se ignora y se registra.
4. Toda tabla de negocio tiene `organization_id NOT NULL` con FK a `organization` y lo incluye en su clave primaria o en un índice único compuesto.
5. Una prueba de esquema consulta `information_schema` y falla si una tabla nueva no tiene `organization_id` (lista explícita de excepciones: `organization`, `schema_migrations`).
6. Una suite reutilizable crea dos organizaciones y verifica que la clave de A reciba `404` al leer, modificar o borrar recursos de B. F3–F8 deben añadir sus endpoints a esta suite.

### Dinero

```go
type Money struct {
    Minor    int64  // unidades menores: MXN 60,00 → 6000
    Currency string // ISO 4217
}
```

- Sumar o restar divisas distintas devuelve error; los desbordamientos de `int64` también.
- `MulRate(rate string, mode RoundingMode)` parsea la tasa como decimal exacto (`big.Rat`), multiplica y redondea con `HALF_UP` a la escala de la divisa. Es la única vía de aplicar porcentajes y la usará F5.
- JSON: `{"amount": "60.00", "currency": "MXN"}`; el importe viaja como string decimal, nunca como número.
- Una prueba de arquitectura recorre el AST de `internal/` y falla si aparece `float32` o `float64` fuera de una lista de excepciones vacía por defecto. Otra consulta `information_schema` y falla ante columnas `real` o `double precision`.

### Relación con el motor

- `rulefare rules …` sigue sin abrir BD ni HTTP; se conserva su prueba y se añade una que lo verifica con `RULEFARE_DATABASE_URL` inválida.
- `architecture_test.go` falla si `engine` importa cualquier paquete que no sea stdlib o `github.com/google/cel-go/...`, o cualquier paquete del módulo.
- Las decisiones de integración con el motor quedan asignadas: caché de `Program` por organización y versión de ruleset en F4; política ante `CONDITION_ERROR` y validaciones de negocio (importes negativos, códigos válidos, proveedor ↔ contrato) en F4/F5; escala y divisa de `amount_minor` resueltas por `money.Money` desde F2.

## 6. Plan de trabajo

### Semana 1 · Esqueleto, base de datos y salud

**Periodo:** 2026-09-28 → 2026-10-02  
**Hito:** `docker compose up` levanta app + PostgreSQL y `/readyz` responde `200`.  
**Estado:** 🟡 En curso

| ID | Trabajo | Entregable | Estado |
|---|---|---|---|
| T1.1 | Añadir `serve`, config por entorno y apagado ordenado (SIGTERM, timeout) | `cmd/rulefare`, `internal/platform/config.go` | ✅ `serve` con `RULEFARE_HTTP_ADDR` y `RULEFARE_SHUTDOWN_TIMEOUT`, drenado de peticiones y error si vence el plazo; `rules` no lee la config de plataforma; pruebas locales con `-race` |
| T1.2 | Mover el control de dependencias del motor a una prueba Go y ampliarla a la regla de dependencias de F2 | `architecture_test.go`; paso de CI sustituido | ✅ `go list -deps` con reglas para `engine` (stdlib + CEL) e `internal/money` (stdlib); prueba de que detecta violaciones; paso de shell eliminado de CI |
| T1.3 | Pool pgx y migrador con advisory lock | `internal/postgres`, `schema_migrations` | ⬜ |
| T1.4 | Logs JSON, `request_id`, `traceparent`, recuperación de pánicos | middleware en `internal/httpapi` | ⬜ |
| T1.5 | `/healthz` y `/readyz` | handlers + pruebas | ⬜ |
| T1.6 | `Dockerfile` multi-etapa y `compose.yaml` con healthchecks | imagen distroless, `docker compose up --wait` en verde | ⬜ |

### Semana 2 · Tenancy, autenticación y dinero

**Periodo:** 2026-10-05 → 2026-10-09  
**Hito:** dos organizaciones con claves distintas no pueden verse entre sí.  
**Estado:** ⬜ Pendiente

| ID | Trabajo | Entregable | Estado |
|---|---|---|---|
| T2.1 | Migraciones `organization` (con divisa) y `api_key` | `migrations/0001…`, `0002…` | ⬜ |
| T2.2 | CLI de bootstrap: `orgs create`, `apikeys create|revoke` | secreto mostrado una vez; solo hash persistido | ⬜ |
| T2.3 | Middleware de API key → `OrgID` en contexto; `401` uniforme | `internal/tenancy`, `internal/httpapi` | ⬜ |
| T2.4 | Endpoints `/v1/organization` y `/v1/api-keys`; suite de aislamiento reutilizable; prueba de esquema `organization_id` | pruebas de integración contra PostgreSQL | ⬜ |
| T2.5 | `money.Money`, `MulRate` con `HALF_UP`, JSON string y tabla ISO 4217 | `internal/money` con pruebas de tabla y fuzzing | ⬜ |
| T2.6 | Pruebas anti-`float` (AST y `information_schema`) | `architecture_test.go`, prueba de esquema | ⬜ |
| T2.7 | `OpaqueRef` y detector de PII en metadata (email, teléfono, documentos) en modo advertencia | `internal/tenancy/ref.go`; log `pii_suspected` sin el valor | ⬜ |

### Semana 3 · Contrato, UI y CI

**Periodo:** 2026-10-12 → 2026-10-16  
**Hito:** la CI levanta el stack completo y verifica salud, aislamiento y contrato.  
**Estado:** ⬜ Pendiente

| ID | Trabajo | Entregable | Estado |
|---|---|---|---|
| T3.1 | `api/openapi.json` con seguridad, errores y endpoints de F2 | lint Redocly en CI | ⬜ |
| T3.2 | Prueba de paridad: rutas registradas = rutas del spec | `internal/httpapi/openapi_test.go` | ⬜ |
| T3.3 | App React mínima con estado de salud y versión; embebida en el binario | `web/`, `go:embed`, placeholder sin Node | ⬜ |
| T3.4 | CI: servicio PostgreSQL, pruebas de integración, build y lint de `web/`, humo de Compose | `.github/workflows/ci.yml` | ⬜ |
| T3.5 | Documentación: README (arranque local, variables, bootstrap), guía de tenancy para F3+ | `README.md`, `internal/tenancy/README.md` | ⬜ |
| T3.6 | Verificación de cierre y evidencia | `propuesta/mvp/verificacion/<fecha>/` | ⬜ |

## 7. Estrategia de pruebas

| Tipo | Qué cubre | Dónde corre |
|---|---|---|
| Unitarias | config, `Money`, parseo de API keys, problem+json, detector PII | `go test ./...`, sin BD |
| Integración | migraciones desde cero, repositorios, autenticación, aislamiento, `/readyz` | CI con PostgreSQL; local con `RULEFARE_TEST_DATABASE_URL` (se omiten con mensaje explícito si no está) |
| Arquitectura | imports del motor y de `internal/money`; ausencia de `float`; `organization_id` en tablas | `go test ./...` |
| Contrato | lint OpenAPI y paridad de rutas | CI |
| Humo | `docker compose up --wait` + `curl /readyz` + UI servida | CI |
| Fuzzing | `FuzzMoneyParse`, `FuzzMulRate` (sin pánicos, redondeo consistente con `big.Rat`) | CI, 10 s por target |
| Regresión F1 | suite, race, fuzzing, benchmarks y medición de sistema existentes | CI, sin cambios |

Casos obligatorios de `Money`: `HALF_UP` en `.5` positivo y negativo, divisas con exponente 0 y 3, desbordamiento, divisas mezcladas, tasas con más de 9 decimales, y los importes del [Excel de F0](./f0_mocks/README.md) (p. ej., 7 % de MXN 8.000 = `56000` centavos).

## 8. Riesgos y mitigaciones

| Riesgo | Señal | Mitigación |
|---|---|---|
| Fuga entre organizaciones | una consulta sin `organization_id` | `OrgID` obligatorio en firmas, suite de aislamiento y prueba de esquema |
| Supuesto de una divisa por organización incorrecto | agencia real con varias divisas (F0 pendiente) | divisa en `Money` desde el inicio; la restricción vive solo en `organization` |
| Semántica de redondeo distinta a la real | finanzas pide otro modo o redondeo por línea | `RoundingMode` explícito en la firma; cambiar el modo no cambia el tipo |
| Frontend bloquea `go build` | falta Node en CI o en local | placeholder embebido; la UI real se compila en Docker y en su job |
| Migraciones concurrentes | dos instancias arrancan a la vez | advisory lock y prueba con dos migradores en paralelo |
| Filtrado de secretos | API key en logs o errores | se registra solo el prefijo; prueba que busca el secreto en la salida de logs |
| Crecimiento de dependencias | nuevas librerías sin necesidad | solo pgx como dependencia Go nueva; `go mod tidy` y `govulncheck` siguen en CI |
| Regresión del motor | cambios en `engine` durante F2 | F2 no modifica `engine/`; la CI de F1 se conserva completa |

## 9. Criterios de aceptación

Del roadmap:

- [ ] Un comando levanta API, base de datos y frontend localmente.
- [ ] Las migraciones se ejecutan desde cero y hacia la versión actual.
- [ ] Una organización no puede leer ni modificar datos de otra en pruebas automatizadas.
- [ ] Ningún importe financiero usa punto flotante.
- [ ] API, UI y base de datos exponen una verificación de salud.
- [ ] Una prueba de arquitectura impide que el módulo F1 importe paquetes de plataforma.

Adicionales de este plan:

- [ ] `rulefare rules validate|evaluate` funcionan sin base de datos disponible.
- [ ] Las API keys solo se persisten como hash y nunca aparecen completas en logs.
- [ ] `organization_id` nunca se acepta desde el cliente.
- [ ] El contrato OpenAPI pasa lint y coincide con las rutas registradas.
- [ ] `go build ./...` funciona sin Node instalado.
- [ ] La única dependencia Go directa nueva es `pgx`; `govulncheck` sin hallazgos.

## 10. Definition of Done

F2 se marca `✅ Completa` cuando:

1. Todos los criterios de aceptación tienen evidencia en la CI de `main`.
2. La suite de aislamiento y la guía de tenancy están listas para que F3 añada sus entidades.
3. Existe una demostración reproducible:

```sh
docker compose up --build --wait
docker compose exec app rulefare orgs create --name "Horizonte Demo" --currency MXN
docker compose exec app rulefare apikeys create --org <org_id>
curl -s localhost:8080/readyz
curl -s -H "Authorization: Bearer <api_key>" localhost:8080/v1/organization
open http://localhost:8080/
```

4. El roadmap refleja el estado de F2 y enlaza la evidencia de cierre.

## 11. Registro de seguimiento

| Fecha | ID | Estado anterior | Estado nuevo | Evidencia / bloqueo | Responsable |
|---|---|---|---|---|---|
| 2026-09-26 | F2 | — | ⬜ Planificada | Plan inicial con decisiones técnicas, tres semanas de trabajo y criterios de salida; núcleo de cálculo se mantiene en F5 | Tech Lead |
| 2026-09-26 | T1.1 / T1.2 | ⬜ Pendientes | ✅ Implementadas con pruebas locales | `rulefare serve`, `internal/platform` e `internal/httpapi`; `architecture_test.go` sustituye el control de shell en CI; suite, race y vet locales en verde; sin dependencias nuevas | Backend |
