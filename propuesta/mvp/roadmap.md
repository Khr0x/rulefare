# Roadmap de implementación del MVP

> Travel Commission Engine · v1 SaaS para host agencies, redes y consorcios

| Campo | Valor |
|---|---|
| Versión del roadmap | 0.22 |
| Última actualización | 2026-09-22 |
| Ventana objetivo | 2026-08-30 → 2027-02-26 |
| Estado general | 🟡 F0 en curso; ✅ F1 completada técnicamente con CI de `main` verde |
| Release objetivo | MVP v1 para pilotos controlados |
| Equipo asumido | 2 backend, 1 frontend/full-stack y apoyo parcial de producto/diseño |

Las fechas son una línea base, no compromisos comerciales. Deben recalibrarse al cerrar la Fase 0 con el equipo real, los datos de clientes piloto y los riesgos descubiertos.

## Objetivo y fuente de verdad

El MVP debe permitir que una agencia:

1. Cargue productos, contratos y reglas reutilizables.
2. Sepa antes de vender cuánto ganará el agente.
3. Registre la venta y obtenga un cálculo determinista y explicable.
4. Mantenga saldos que se reviertan sin borrar historia ante una cancelación.
5. Suba el CSV del proveedor y vea lo esperado, cobrado y pendiente.

La fuente de verdad para el alcance es [Alcance v1](./travel_commission_engine_alcance_v1.md). La propuesta de negocio se amplía en [Travel Commission Engine](./travel_commission_engine.md), el orden de resolución en [Jerarquía y reglas](./travel_commission_engine_jerarquia_reglas.md), la promesa comercial en [Resumen ejecutivo](./travel_commission_engine_mvp1_resumen_ejecutivo.md) y la relación entre reglas, cálculo y explicación en [el diagrama del motor](./travel_commission_engine_dos_piezas.svg).

### Nomenclatura

- `F0`–`F10` son fases de entrega de este roadmap y las únicas que reciben estado operativo aquí.
- `v1`, `v1 Enterprise`, `v1.5` y `v2+` son bandas de alcance de producto.
- Las “Fases 1–5” comerciales y las oleadas del ecosistema describen go-to-market o expansión, no sustituyen `F0`–`F10`.

## Decisión arquitectónica: construir primero el motor de reglas

El motor de reglas es el primer componente de software que se construirá. La validación de negocio de F0 puede avanzar antes o en paralelo, pero no es una dependencia técnica del motor.

### Contraste con lo prometido

| Requisito | ¿Estaba prometido? | Conclusión y ajuste |
|---|---|---|
| Construirlo primero | No en el roadmap 0.1: aparecía después de plataforma y modelo | Se corrige: pasa a F1 y es la primera fase de construcción |
| No depender de otros módulos | Parcialmente: se promete un core único, determinista y embebible, pero no se había fijado su frontera | El core no importará persistencia, HTTP, UI, multi-tenancy ni tipos del catálogo; recibe reglas y contexto como datos |
| Ser muy rápido | Se describe prefiltro + CEL y cálculo síncrono, pero sin umbrales medibles | F1 incorpora benchmarks y presupuestos de latencia |
| Ocupar pocos recursos | La arquitectura evita Redis, NATS, Kafka y Kubernetes en v1, pero no establece un presupuesto | F1 incorpora límites de memoria y CPU en un entorno reproducible |
| Ser nativo | Sí: Go y un mismo binario; CEL también está comprometido como lenguaje de condiciones | «Nativo» significa binario Go compilado, sin VM ni servicio auxiliar. No significa cero librerías: CEL queda embebido en el binario |

### Frontera obligatoria del motor

```text
ENTRADA                         MOTOR DE REGLAS                  SALIDA
ruleset versionado      ──────►  compilar / validar       ──────►  regla(s) resuelta(s)
contexto normalizado    ──────►  match CEL                ──────►  outcome tipado
fecha efectiva          ──────►  ranking lexicográfico   ──────►  EvaluationTrace
```

El motor no conoce PostgreSQL, archivos, red, API keys, organizaciones, bookings ni React. El host adapta esos conceptos al contrato de entrada. La compilación CEL ocurre al cargar una versión; la ruta caliente solo evalúa programas ya compilados.

### Presupuesto inicial de rendimiento

Estos valores son objetivos de ingeniería que F1 debe validar, no cifras comerciales, medidos con 1 vCPU y límite de 512 MiB:

| Métrica | Objetivo F1 |
|---|---:|
| Evaluación caliente con hasta 100 reglas candidatas precompiladas | p95 ≤ 5 ms; p99 ≤ 10 ms |
| Memoria residente con 10.000 reglas precompiladas | ≤ 128 MiB |
| CPU en reposo | ≈ 0 |
| I/O durante una evaluación | 0 accesos a red, disco o base de datos |
| Repetibilidad | mismo input + ruleset produce resultado y trace idénticos |

Si el benchmark no cumple, F1 no se cierra: primero se perfila y se elimina el cuello observado. No se añade caché o infraestructura distribuida por anticipado.

## Arquitectura propuesta del MVP

### Principios

1. **Un solo core.** Toda resolución de reglas ocurre en `engine`; ningún módulo replica esa lógica.
2. **Dependencias hacia adentro.** La aplicación conoce al motor; el motor no conoce a la aplicación ni a travel.
3. **Una unidad desplegable.** Un binario Go sirve API, jobs y frontend React compilado.
4. **Un único servicio de datos.** PostgreSQL almacena configuración, historial, jobs y movimientos.
5. **Cálculo síncrono.** `SIMULATE` y `COMMIT` responden en la petición; solo las importaciones usan jobs.
6. **Historia inmutable.** Reglas publicadas, cálculos y movimientos no se reescriben.
7. **Sin infraestructura especulativa.** No Redis, broker, microservicios ni Kubernetes en el MVP.

### Vista lógica

```text
 Navegador / cliente API
          │
          │ HTTPS · REST/JSON
          ▼
┌─────────────────────────────────────────────────┐
│             APLICACIÓN GO · UN PROCESO             │
│                                                   │
│  HTTP API + React estático + autenticación/API key  │
│                       │                           │
│                       ▼                           │
│  ┌─────────── MÓDULOS DE APLICACIÓN ───────────┐  │
│  │ catálogo · contratos · importación · jobs   │  │
│  │ cálculo · lifecycle · saldos · conciliación  │  │
│  └──────────────────┬───────────────────────┘  │
│                    │                              │
│       reglas + contexto normalizado                  │
│                    ▼                              │
│  ┌────────── ENGINE INDEPENDIENTE ──────────┐       │
│  │ Go + CEL · función pura · sin I/O           │       │
│  │ match · ranking · outcome · trace           │       │
│  └─────────────────────────────────────────┘       │
│                                                   │
│  Repositorios PostgreSQL · transacciones · job runner  │
└──────────────────────────────────────────────┬──┘
                                               │
                                               ▼
                                        ┌─────────────┐
                                        │ PostgreSQL  │
                                        └─────────────┘
```

### Módulos y responsabilidades

| Módulo | Responsabilidad | Puede depender de |
|---|---|---|
| `engine` | Compilar CEL, hacer match, ranking, outcomes y trace | Go stdlib + CEL |
| `catalog` | Productos, paquetes, proveedores y contratos | PostgreSQL; tipos propios |
| `rules` | Versiones, vigencia, borradores, publicación e importación | `engine`, PostgreSQL |
| `calculation` | Normalizar contexto y ejecutar la cadena `basis → contract → gross → split → net` | `engine`, `catalog`, PostgreSQL |
| `ledger` | Lifecycle, movimientos append-only, reversos y saldos | PostgreSQL; resultados de `calculation` |
| `reconciliation` | Statements CSV y expected vs received | PostgreSQL; cálculos confirmados |
| `httpapi` | REST, autenticación, validación de entrada y errores | Módulos de aplicación |
| `jobs` | Importaciones CSV/Excel idempotentes con tabla de jobs | `rules`, `catalog`, `reconciliation`, PostgreSQL |
| `web` | Commission Cloud en React | Solo HTTP API; nunca lógica financiera local |

Regla de imports: `engine` no importa ningún otro paquete del proyecto. Los módulos no acceden directamente a tablas ajenas para ejecutar lógica de negocio; coordinan mediante funciones de aplicación y una transacción compartida cuando la atomicidad lo exige.

### Estructura inicial del repositorio

```text
/
├── cmd/rulefare/       # arranque del único binario
├── engine/            # motor de reglas standalone; sin imports internos
├── internal/
│   ├── catalog/
│   ├── rules/
│   ├── calculation/
│   ├── ledger/
│   ├── reconciliation/
│   ├── httpapi/
│   ├── jobs/
│   └── postgres/
├── migrations/        # esquema PostgreSQL versionado
├── web/               # fuente React; build embebido con go:embed
└── compose.yaml       # app + PostgreSQL
```

Todo vive inicialmente en un solo módulo Go. Separar `engine` en otro repositorio o microservicio solo se considera si existe un consumidor externo real que lo justifique.

### Flujos principales

**SIMULATE · sin efectos**

```text
HTTP → autenticar org → cargar producto/contrato/reglas
     → normalizar Context → engine.Evaluate
     → CalculationResult + EvaluationTrace → respuesta
```

**COMMIT · transacción financiera**

```text
HTTP → validar external_id → engine.Evaluate
     → BEGIN
       guardar booking + snapshot de ruleset + calculation + movements
       registrar idempotency key
     → COMMIT → respuesta
```

**Importación de reglas**

```text
Excel/CSV → job persistido → mapear filas → engine.Validate
          → ruleset DRAFT + reporte de errores → revisión → publicar versión
```

**Conciliación**

```text
CSV proveedor → job persistido → match por external_id
              → expected vs received vs variance → revisión manual
```

### Datos y garantías

- `organization_id` forma parte de toda clave y consulta multi-tenant; proviene de la credencial.
- Dinero se representa como decimal + divisa y se redondea explícitamente; nunca `float`.
- Reglas publicadas y contratos se versionan con `valid_from` / `valid_to`.
- Cada cálculo guarda input normalizado, versión, resultado y trace.
- Cada movimiento es append-only; corregir significa añadir un reverso o ajuste.
- `external_id` + organización protege `COMMIT` e importaciones contra duplicados.
- La ruta financiera se confirma en una sola transacción PostgreSQL.
- El core no ingiere PII: usa referencias opacas.

### Despliegue y operación

```text
Docker Compose
├── app       binario Go + assets React + job runner
└── postgres  única persistencia
```

- Configuración mediante YAML y variables de entorno; secretos fuera del repositorio.
- Logs JSON y OpenTelemetry básico desde F2.
- Backups y restauración de PostgreSQL validados antes del piloto.
- Escalado inicial: una instancia de app; se replica solo si las mediciones lo exigen.
- Sin Redis, NATS, Kafka, Temporal, Kubernetes, Helm ni servicios de pago en v1.

### Evolución de la arquitectura por fase

| Fase | Incremento arquitectónico |
|---|---|
| F1 | Paquete `engine`, ejecutable `rulefare` con subcomandos `rules`, pruebas y benchmarks sin infraestructura |
| F2 | Subcomando `rulefare serve` en el mismo binario, HTTP, React embebido, PostgreSQL y tenancy |
| F3 | `catalog` y adaptador travel → contexto neutral del engine |
| F4 | `rules` + `jobs` para importar, validar y publicar versiones |
| F5 | `calculation` y transacción idempotente de `COMMIT` |
| F6 | `ledger` append-only y saldos derivados |
| F7 | Flujos completos de Commission Cloud |
| F8 | `reconciliation` y jobs de statements CSV |
| F9 | Pruebas de arquitectura, seguridad, recuperación y rendimiento |

### Referencias

- [Alcance v1 · Dos modos de consumo](./travel_commission_engine_alcance_v1.md#dos-modos-de-consumo-el-mismo-motor-dos-formas-de-venderlo)
- [Alcance v1 · Cálculo](./travel_commission_engine_alcance_v1.md#bloque-3--cálculo-antes-y-después-de-vender)
- [Alcance v1 · Seguridad y datos](./travel_commission_engine_alcance_v1.md#4-seguridad-y-datos-en-v1)
- [Alcance v1 · Stack](./travel_commission_engine_alcance_v1.md#5-stack-de-la-v1)
- [Jerarquía · Performance](./travel_commission_engine_jerarquia_reglas.md#8-performance-prefiltro-y-cel-final)

## Estados

| Estado | Significado |
|---|---|
| ⬜ Pendiente | No ha comenzado |
| 🟡 En curso | Existe trabajo activo y evidencia parcial |
| 🟦 En validación | Construcción terminada; faltan pruebas o aceptación |
| ✅ Completa | Cumple todos los criterios de salida |
| 🛑 Bloqueada | No puede avanzar; el bloqueo y responsable están registrados |

Una fase solo pasa a `Completa` cuando cumple todos sus criterios de salida. El porcentaje subjetivo no reemplaza esta regla.

## Corte verificable al 2026-09-22

El corte local siguiente conserva su evidencia histórica. El estado actual de F1 se verifica en el [cierre técnico del merge `09ff1e2`](./verificacion/2026-09-22/README.md#cierre-técnico-de-f1), con CI remota y artefacto de mediciones.

| Comprobación | Resultado en el árbol actual |
|---|---|
| `go build ./...` con Go 1.27.1 | ✅ Pasa |
| `gofmt -l engine cmd` y `git diff --check` | ✅ Sin diferencias de formato ni errores de whitespace |
| `go test ./...` | ✅ Pasa, incluidas las regresiones de T2.7, sanitización del trace, CLI y concurrencia |
| `go test -race ./...` y `go vet ./...` | ✅ Pasan |
| `go mod tidy -diff` | ✅ Sin diferencias |
| `govulncheck ./...` | No reejecutado en este corte; el workflow lo incluye |

El error de import y formato del 2026-09-04 ya está corregido. El ganador se resuelve por especificidad y prioridad; un empate persistente devuelve `AMBIGUOUS_MATCH`. El resultado clona su outcome.

| Alcance | Estado comprobado |
|---|---|
| F0 | En curso; sus entrevistas, clientes de diseño y aprobación financiera no tienen evidencia en este repositorio |
| F1 | Contrato, validación, CEL, filtros, ranking, empates, `NO_MATCH`, copia del outcome, T2.7, trace sanitizado, CLI, pruebas concurrentes, fuzzing y benchmarks implementados con evidencia local |
| F1 cierre técnico | ✅ CI de `main` para `09ff1e2` en verde con corpus de 32 casos y artefacto de benchmarks; tarifas reales fuera de F1 |
| F2–F10 | Sin implementación en el árbol actual |

T2.7 valida toda la entrada antes de resolver reglas: capa conocida, fecha explícita y contexto completo conforme al schema. Los errores devuelven `INVALID_INPUT` sin candidatos ni fallback. El contrato de tipos y normalización está documentado en el [README](../../README.md#contrato-de-evaluación-t27).

T2.5 ya omite el mensaje interno de CEL: el trace expone únicamente `rule_id` y `CONDITION_ERROR` para esos errores, con pruebas de privacidad y estabilidad del JSON. T3.1 y T3.2 ya implementan `rulefare rules validate/evaluate`, con límite de 8 MiB por archivo JSON, reportes y códigos de salida probados. T3.3 añade pruebas concurrentes dedicadas con `-race`, tanto de lectura como de mutación de valores devueltos, con resultados locales en verde. T3.4 incorpora tres targets de fuzzing para rulesets, CEL/scopes y contextos, con semillas versionadas, campañas locales sin fallos y campañas breves configuradas en CI. T3.5 ya tiene una [baseline reproducible](./benchmarks/2026-09-22/README.md): en Linux arm64 con 1 CPU/512 MiB, p95/p99 de 0,497/0,643 ms para 100 candidatas; RSS tras GC de 136,7 MiB con 10.000 reglas. El [perfilado T3.6](./benchmarks/2026-09-22/t3.6/README.md) identifica programas CEL duplicados: reutilizar condiciones idénticas dentro de cada compilación reduce el RSS a 29,5 MiB y mantiene la latencia en objetivo (p95/p99 0,409/0,435 ms). La [segunda optimización T3.6](./benchmarks/2026-09-22/t3.6-unique/README.md) reduce el control de condiciones únicas a 86,4 MiB de RSS mediano al cargar únicamente las funciones CEL usadas; el caso repetido registra 31,4 MiB y la latencia final p95/p99 es 0,417/0,434 ms. T3.6 queda completada localmente. [T3.9](./benchmarks/2026-09-22/t3.9/README.md) añade cotas de payload, decimales, coste individual/acumulado y regex previas al matcher, con errores bloqueantes y pruebas adversariales. La primera medición aumentó el tiempo y las asignaciones. La [optimización posterior](./benchmarks/2026-09-22/t3.9-optimization/README.md) mantiene los límites y corrige la regresión de la baseline: 100 candidatas repetidas en 19,8 µs, únicas en 124,1 µs; p95/p99 de repetidas 0,042/0,057 ms y RSS único mediano 94,6 MiB. T3.8 completa la [guía de API](../../engine/README.md), contratos, trace, errores y límites, con ejemplos Go/CLI verificados localmente. La [medición de sistema](./benchmarks/2026-09-22/system/README.md) completa la evidencia local de CPU en reposo (mediana 0,002676 %) y ausencia de I/O de archivos/red en 4.100 evaluaciones observadas. El [corpus técnico](../../engine/testdata/README.md) contiene 32 casos con snapshots explícitos y prueba de orden invertido (64 evaluaciones). Un agente independiente con enfoque financiero completó la [revisión de dominio](../../engine/testdata/FINANCIAL_REVIEW.md) solicitada por el responsable ante la ausencia de equipo de producto/finanzas. La [CI del merge `09ff1e2`](https://github.com/Khr0x/rulefare/actions/runs/35778045359) pasó con esta ampliación y publicó el artefacto de benchmarks. Los criterios de salida técnicos de F1 están cumplidos; las tarifas reales requieren aceptación comercial separada. F2 depende también de F0.

## Calendario maestro

| ID | Fase | Inicio objetivo | Fin objetivo | Duración | Estado | Inicio real | Fin real | Depende de |
|---|---|---:|---:|---:|---|---:|---:|---|
| F0 | Validación y congelamiento del alcance | 2026-08-30 | 2026-09-11 | 2 semanas | 🟡 En curso | 2026-08-30 | — | — |
| F1 | Motor de reglas nativo e independiente | 2026-09-14 | 2026-10-02 | 3 semanas | ✅ Completa técnicamente | 2026-08-30 | 2026-09-22 | Ninguna |
| F2 | Fundación de plataforma y multi-tenant | 2026-10-05 | 2026-10-16 | 2 semanas | ⬜ Pendiente | — | — | F0, F1 |
| F3 | Modelo travel, catálogo y contratos | 2026-10-19 | 2026-10-30 | 2 semanas | ⬜ Pendiente | — | — | F2 |
| F4 | Importación de reglas desde Excel | 2026-11-02 | 2026-11-13 | 2 semanas | ⬜ Pendiente | — | — | F1, F3 |
| F5 | Motor de cálculo, SIMULATE y COMMIT | 2026-11-16 | 2026-12-04 | 3 semanas | ⬜ Pendiente | — | — | F1, F3 |
| F6 | Lifecycle, movimientos y saldos | 2026-12-07 | 2026-12-18 | 2 semanas | ⬜ Pendiente | — | — | F5 |
| — | Contingencia y cierre anual | 2026-12-21 | 2027-01-01 | 2 semanas | Reserva | — | — | — |
| F7 | Commission Cloud y experiencia del agente | 2027-01-04 | 2027-01-15 | 2 semanas | ⬜ Pendiente | — | — | F2, F4, F6 |
| F8 | Conciliación básica por CSV | 2027-01-18 | 2027-01-29 | 2 semanas | ⬜ Pendiente | — | — | F6, F7 |
| F9 | Endurecimiento y release candidate | 2027-02-01 | 2027-02-12 | 2 semanas | ⬜ Pendiente | — | — | F8 |
| F10 | Pilotos y liberación del MVP | 2027-02-15 | 2027-02-26 | 2 semanas | ⬜ Pendiente | — | — | F9 |

## F0 · Validación y congelamiento del alcance

**Periodo:** 2026-08-30 → 2026-09-11  
**Estado:** 🟡 En curso  
**Responsables:** Producto + Tech Lead  
**Dependencias:** Ninguna

### Trabajo

- Validar el problema y la demostración con 2–3 agencias del perfil objetivo.
- Obtener, anonimizados, un Excel de contratos/reglas, reservas de ejemplo y un statement de proveedor.
- Convertir esos datos en un corpus dorado con resultados esperados.
- Acordar una sola divisa operativa por organización para v1.
- Congelar la lista de capacidades v1 y registrar cualquier solicitud adicional fuera del MVP.
- Definir las métricas iniciales: tiempo de alta, diferencias detectadas, cálculos explicados y tiempo de back-office.

### Criterios de salida

- [ ] Hay al menos dos clientes de diseño disponibles para el piloto.
- [ ] Existen casos dorados para producto simple, paquete, split host/advisor, tiered, cancelación y conciliación.
- [ ] Cada caso tiene importes esperados aprobados por una persona de finanzas.
- [ ] El alcance v1 y la demostración de diez minutos están aceptados.

### Referencias

- [Resumen ejecutivo · Mercado objetivo](./travel_commission_engine_mvp1_resumen_ejecutivo.md#5-mercado-objetivo)
- [Resumen ejecutivo · Validación](./travel_commission_engine_mvp1_resumen_ejecutivo.md#10-validación-demostración-de-10-minutos)
- [Alcance v1 · Principio rector](./travel_commission_engine_alcance_v1.md#1-principio-rector-del-corte)

## F1 · Motor de reglas nativo e independiente

**Periodo:** 2026-09-14 → 2026-10-02  
**Estado:** 🟡 En curso · evaluación provisional; suite actual en rojo  
**Responsables:** Backend  
**Dependencias:** Ninguna

### Entregable

Un módulo Go importable y un único ejecutable `rulefare`, con entrada en `cmd/rulefare/`. La CLI expone `rulefare rules validate` y `rulefare rules evaluate`: valida archivos JSON y evalúa reglas devolviendo el outcome y `EvaluationTrace` sin levantar servicios externos. F2 ampliará el mismo ejecutable con `rulefare serve`; el paquete `engine` seguirá independiente de la plataforma.

### Trabajo

- Definir un contrato de entrada neutral: `RuleSet`, `Rule`, `Scope`, `Context`, `EffectiveAt` y versión.
- Definir salidas tipadas: `percentage`, `fixed`, `tiered`, error de cobertura y `EvaluationTrace`.
- Compilar y validar CEL una sola vez al cargar cada versión; reutilizar el programa compilado.
- Validar y normalizar `Layer`, `EffectiveAt` y `Context` contra el schema antes de evaluar; un contexto inválido debe fallar cerrado.
- Implementar jerarquía configurable por una lista ordenada de dimensiones, sin importar tipos del dominio travel.
- Resolver por especificidad lexicográfica y `FIRST_MATCH`.
- Usar prioridad explícita solo como desempate y fallar ante un empate persistente.
- Mantener el motor como función pura: sin red, disco, reloj global, variables de entorno, base de datos ni estado compartido mutable.
- Devolver resultados desacoplados del `Program`, de modo que el consumidor no pueda mutar el estado compilado.
- Limitar el coste runtime de CEL y el tamaño de valores/contextos antes de exponer el motor a entrada no confiable.
- Dejar el prefiltro de persistencia fuera del core; el host entrega las reglas candidatas.
- Añadir pruebas de determinismo, concurrencia, empates, vigencia, CEL inválido y corpus dorado inicial.
- Añadir benchmarks reproducibles de latencia, memoria y asignaciones, con resultados guardados como artefacto de CI.

### Criterios de salida

- [x] El paquete se prueba con `go test` y se ejecuta con una CLI sin PostgreSQL, Docker, HTTP ni frontend (pruebas locales y binario ejecutado).
- [x] Los imports directos de `engine` son estándar o CEL; no importa dependencias de infraestructura ([control en CI](https://github.com/Khr0x/rulefare/actions/runs/35778045359)). Las dependencias transitivas de CEL permanecen en go.mod/go.sum.
- [x] El mismo input y ruleset producen bytes de salida equivalentes, incluido el orden del trace (corpus de 32 casos en orden original/invertido y pruebas concurrentes con `-race` en CI).
- [x] Varias evaluaciones concurrentes no comparten ni corrompen estado mutable por llamada (T3.3, pruebas locales con `-race`).
- [x] Mutar un resultado no altera el `Program` ni evaluaciones posteriores (prueba local).
- [x] Un layer, fecha o contexto inválido devuelve un error explícito y nunca activa una regla fallback (T2.7, prueba local).
- [x] Un empate exacto falla de forma explícita (prueba local).
- [x] Un contexto válido sin regla aplicable devuelve `NO_MATCH`, nunca una comisión cero inventada (prueba local).
- [x] Los escenarios medidos cumplen los presupuestos de latencia, memoria, CPU en reposo e I/O; corpus técnico revisado por IA y [CI de `main`](https://github.com/Khr0x/rulefare/actions/runs/35778045359) verde.
- [x] El benchmark se ejecuta en CI y publica un artefacto ligado al SHA del run; la comparación automática de regresiones aún no está implementada y queda como mejora posterior.
- [x] Los límites de payload y coste CEL detienen entradas que exceden el presupuesto (T3.9, evidencia local).

### Referencias

- [Plan de implementación de F1](./f1_motor_reglas_plan_implementacion.md)
- [Alcance v1 · Las dos piezas del motor](./travel_commission_engine_alcance_v1.md#las-dos-piezas-del-motor)
- [Alcance v1 · Reglas reutilizables](./travel_commission_engine_alcance_v1.md#bloque-2--reglas-reutilizables-sobre-productos)
- [Jerarquía · Algoritmo de evaluación](./travel_commission_engine_jerarquia_reglas.md#5-algoritmo-de-evaluación)
- [Jerarquía · Performance](./travel_commission_engine_jerarquia_reglas.md#8-performance-prefiltro-y-cel-final)
- [Jerarquía · Qué no construir](./travel_commission_engine_jerarquia_reglas.md#9-qué-no-construir)

## F2 · Fundación de plataforma y multi-tenant

**Periodo:** 2026-10-05 → 2026-10-16  
**Estado:** ⬜ Pendiente  
**Responsables:** Backend + Frontend/Full-stack  
**Dependencias:** F0, F1

### Trabajo

- Crear el monolito modular en Go, PostgreSQL, contrato OpenAPI y aplicación React mínima alrededor del motor, sin introducir dependencias desde F1 hacia la plataforma.
- Añadir `rulefare serve` al ejecutable existente para iniciar la aplicación; los subcomandos `rules` seguirán funcionando sin inicializar servicios de plataforma.
- Dejar un entorno reproducible con Docker Compose, migraciones y configuración por variables de entorno.
- Implementar `organization_id` en todas las entidades y obtenerlo de la API key, nunca del body.
- Definir `Money {amount, currency}` con decimal y redondeo `HALF_UP`; prohibir `float` para dinero.
- Incorporar logs estructurados, trazas básicas y health checks.
- Modelar `customer_ref` como referencia opaca y advertir sobre posibles datos personales en metadata.

### Criterios de salida

- [ ] Un comando levanta API, base de datos y frontend localmente.
- [ ] Las migraciones se ejecutan desde cero y hacia la versión actual.
- [ ] Una organización no puede leer ni modificar datos de otra en pruebas automatizadas.
- [ ] Ningún importe financiero usa punto flotante.
- [ ] API, UI y base de datos exponen una verificación de salud.
- [ ] Una prueba de arquitectura impide que el módulo F1 importe paquetes de plataforma.

### Referencias

- [Alcance v1 · Multi-tenant](./travel_commission_engine_alcance_v1.md#bloque-5--multi-tenant)
- [Alcance v1 · Seguridad y datos](./travel_commission_engine_alcance_v1.md#4-seguridad-y-datos-en-v1)
- [Alcance v1 · Stack](./travel_commission_engine_alcance_v1.md#5-stack-de-la-v1)

## F3 · Modelo travel, catálogo y contratos

**Periodo:** 2026-10-19 → 2026-10-30  
**Estado:** ⬜ Pendiente  
**Responsables:** Backend + Producto  
**Dependencias:** F2

### Trabajo

- Implementar organización, agente, sucursal, proveedor, producto, contrato de proveedor y booking.
- Tratar `supplier_contract` como entidad versionada con vigencia, divisa y términos comisionables.
- Crear el catálogo multi-vertical con alta manual y CSV.
- Modelar base comisionable por porcentaje plano o exclusiones de líneas.
- Permitir paquetes compuestos; cada componente conserva producto, importe y base propios.
- Adaptar el modelo travel al `Context` neutral de F1; no añadir imports travel al motor.
- Exponer CRUD mínimo por API y pantallas administrativas simples.

### Criterios de salida

- [ ] Se puede registrar un producto simple y un paquete con varios componentes.
- [ ] Cambiar un contrato crea una versión nueva y no altera su historial.
- [ ] La base comisionable queda congelada al registrar el booking.
- [ ] Los casos dorados de catálogo y contrato pasan.
- [ ] El adaptador transforma un booking en contexto del motor sin modificar F1.

### Referencias

- [Alcance v1 · Catálogo de productos](./travel_commission_engine_alcance_v1.md#bloque-1--catálogo-de-productos)
- [Alcance v1 · Modelo de datos](./travel_commission_engine_alcance_v1.md#3-modelo-de-datos-v1)
- [Propuesta · Modelo de datos básico](./travel_commission_engine.md#16-modelo-de-datos-básico)

## F4 · Importación de reglas desde Excel

**Periodo:** 2026-11-02 → 2026-11-13  
**Estado:** ⬜ Pendiente  
**Responsables:** Backend + Frontend/Full-stack  
**Dependencias:** F1, F3

### Trabajo

- Importar hojas de contratos, tarifas y splits mediante mapeo de columnas.
- Generar reglas en estado borrador; nunca activarlas directamente desde el archivo.
- Mostrar errores por fila, vista previa y cobertura antes de publicar.
- Permitir corregir, validar y publicar una versión completa de reglas.
- Registrar quién importó y publicó cada versión.

### Criterios de salida

- [ ] Un archivo representativo del cliente produce borradores válidos y errores accionables.
- [ ] Ninguna fila inválida activa reglas parciales por accidente.
- [ ] El usuario puede revisar y publicar sin intervención técnica.
- [ ] El proceso completo forma parte de la demostración de diez minutos.

### Referencias

- [Alcance v1 · Onboarding de reglas](./travel_commission_engine_alcance_v1.md#onboarding-de-reglas-el-importador-de-excel-hace-real-la-promesa)
- [Resumen ejecutivo · La solución](./travel_commission_engine_mvp1_resumen_ejecutivo.md#3-la-solución)

## F5 · Motor de cálculo, SIMULATE y COMMIT

**Periodo:** 2026-11-16 → 2026-12-04  
**Estado:** ⬜ Pendiente  
**Responsables:** Backend  
**Dependencias:** F1, F3

### Trabajo

- Implementar una función pura y determinista para recorrer `basis → contract → gross → split → net`.
- Calcular cada componente de un paquete y agregar el resultado sin perder su trace.
- Exponer `SIMULATE` para vista previa sin efectos y `COMMIT` para confirmar una venta.
- Hacer `COMMIT` idempotente mediante `external_id` por organización.
- Persistir input, versión de reglas, resultado, redondeos y trace de cada nodo.
- Exponer consulta del cálculo histórico y la respuesta de «¿Por qué?».

### Criterios de salida

- [ ] `SIMULATE` y `COMMIT` devuelven el mismo importe para la misma entrada y versión.
- [ ] `SIMULATE` no genera ventas, movimientos ni saldos.
- [ ] Reintentar `COMMIT` no duplica la venta.
- [ ] Producto simple, paquete, porcentaje, fijo, tiered y split pasan el corpus dorado.
- [ ] Un cálculo histórico conserva su explicación tras publicar reglas nuevas.

### Referencias

- [Alcance v1 · Cálculo antes y después de vender](./travel_commission_engine_alcance_v1.md#bloque-3--cálculo-antes-y-después-de-vender)
- [Propuesta · Motor de cálculo](./travel_commission_engine.md#4-motor-de-cálculo)
- [Propuesta · Explainability](./travel_commission_engine.md#17-explainability)

## F6 · Lifecycle, movimientos y saldos

**Periodo:** 2026-12-07 → 2026-12-18  
**Estado:** ⬜ Pendiente  
**Responsables:** Backend  
**Dependencias:** F5

### Trabajo

- Implementar movimientos append-only por beneficiario y saldos derivados.
- Incorporar los estados `ESTIMATED`, `CONFIRMED`, `ACCRUED`, `PAYABLE`, `PAID`, `CANCELLED`, `DISPUTED`, `ADJUSTED` y `CLAWBACK`.
- Proteger las transiciones manuales/API permitidas.
- Revertir cancelaciones mediante un movimiento negativo referenciado; nunca borrar ni mutar el original.
- Permitir marcado manual de pago y exportación CSV del estado de cuenta del agente.

### Criterios de salida

- [ ] El saldo siempre puede reconstruirse desde los movimientos.
- [ ] Una cancelación reduce el saldo exactamente una vez.
- [ ] No existe una operación que elimine o reescriba historia financiera.
- [ ] Las transiciones inválidas fallan y dejan evidencia en logs.
- [ ] El estado de cuenta coincide con los casos dorados.

### Referencias

- [Alcance v1 · Saldos](./travel_commission_engine_alcance_v1.md#saldos-v1-sin-doble-partida)
- [Alcance v1 · Ciclo de vida](./travel_commission_engine_alcance_v1.md#ciclo-de-vida-v1)
- [Alcance v1 · Reversión por cancelación](./travel_commission_engine_alcance_v1.md#reversión-automática-por-cancelación)
- [Propuesta · Lifecycle](./travel_commission_engine.md#5-lifecycle-de-la-comisión)

## F7 · Commission Cloud y experiencia del agente

**Periodo:** 2027-01-04 → 2027-01-15  
**Estado:** ⬜ Pendiente  
**Responsables:** Frontend/Full-stack + Producto/Diseño  
**Dependencias:** F2, F4, F6

### Trabajo

- Completar el flujo administrativo de catálogo, contratos, reglas, importación y publicación.
- Crear la vista del agente con productos y su comisión antes de vender.
- Mostrar cálculos, saldos, movimientos y estado de cuenta por periodo.
- Crear la vista «¿Por qué?» con la cadena y el trace por nodo.
- Incorporar un dashboard básico por proveedor y agente.
- Cubrir estados vacíos, errores, navegación por teclado y contraste accesible.

### Criterios de salida

- [ ] Un usuario no técnico completa el flujo producto → regla → simulación.
- [ ] El agente ve su importe y la retención de la agencia antes de vender.
- [ ] Finanzas puede rastrear cualquier total hasta sus cálculos y reglas.
- [ ] Los flujos críticos funcionan con teclado y muestran errores accionables.

### Referencias

- [Alcance v1 · Lo que ve el agente](./travel_commission_engine_alcance_v1.md#bloque-4--lo-que-ve-el-agente)
- [Propuesta · Dashboard financiero](./travel_commission_engine.md#7-dashboard-financiero)
- [Resumen ejecutivo · Propuesta de valor](./travel_commission_engine_mvp1_resumen_ejecutivo.md#4-propuesta-de-valor-de-ahorrar-trabajo-a-encontrar-dinero)

## F8 · Conciliación básica por CSV

**Periodo:** 2027-01-18 → 2027-01-29  
**Estado:** ⬜ Pendiente  
**Responsables:** Backend + Frontend/Full-stack  
**Dependencias:** F6, F7

### Trabajo

- Importar statements CSV por proveedor y periodo.
- Hacer matching determinista por `external_id` y permitir corrección manual.
- Comparar `expected_amount`, `received_amount` y `variance` por reserva/línea.
- Mostrar cobrado, pendiente, diferencia y líneas sin correspondencia.
- Permitir marcado manual como conciliado y drill-down al cálculo esperado.
- Agregar el resumen mensual esperado vs. recibido vs. pendiente.

### Criterios de salida

- [ ] El statement del piloto se importa sin alterar cálculos existentes.
- [ ] Cada diferencia muestra importe esperado, recibido y origen del esperado.
- [ ] Las líneas duplicadas o inválidas no se aplican dos veces.
- [ ] El total del dashboard cuadra con las líneas conciliadas.
- [ ] La demostración responde «cuánto falta por cobrar».

### Referencias

- [Alcance v1 · Reconciliación](./travel_commission_engine_alcance_v1.md#el-otro-lado-de-la-cadena-reconciliación-básica-en-v1-rica-en-v15)
- [Propuesta · Reconciliación](./travel_commission_engine.md#6-reconciliación)
- [Resumen ejecutivo · Alcance del MVP](./travel_commission_engine_mvp1_resumen_ejecutivo.md#7-alcance-del-mvp-y-hoja-de-ruta)

## F9 · Endurecimiento y release candidate

**Periodo:** 2027-02-01 → 2027-02-12  
**Estado:** ⬜ Pendiente  
**Responsables:** Equipo completo  
**Dependencias:** F8

### Trabajo

- Automatizar de extremo a extremo la demostración de diez minutos.
- Ejecutar pruebas de aislamiento multi-tenant, idempotencia, redondeo, concurrencia y regresión financiera.
- Ejecutar los benchmarks de F1 y bloquear el release ante una regresión fuera del presupuesto acordado.
- Validar restauración de base de datos, migraciones y operación del entorno.
- Revisar ausencia de PII, secretos y permisos excesivos.
- Instrumentar errores, latencias, jobs fallidos e importaciones rechazadas.
- Medir con el volumen de los pilotos y corregir solo cuellos de botella observados.
- Congelar alcance y publicar notas del release candidate.

### Criterios de salida

- [ ] Todos los casos dorados y recorridos E2E pasan en CI.
- [ ] El motor sigue cumpliendo sus presupuestos de latencia, memoria e I/O dentro de la plataforma completa.
- [ ] No hay defectos abiertos que puedan cambiar importes, duplicar movimientos o cruzar organizaciones.
- [ ] Una restauración probada recupera datos y trazabilidad.
- [ ] El equipo puede diagnosticar una importación o cálculo fallido con logs y trace.
- [ ] Producto y finanzas aprueban el release candidate.

### Referencias

- [Alcance v1 · Clasificación completa](./travel_commission_engine_alcance_v1.md#7-clasificación-completa-por-componente)
- [Jerarquía · Validación previa](./travel_commission_engine_jerarquia_reglas.md#7-validación-previa-al-deploy)
- [Resumen ejecutivo · Demostración](./travel_commission_engine_mvp1_resumen_ejecutivo.md#10-validación-demostración-de-10-minutos)

## F10 · Pilotos y liberación del MVP

**Periodo:** 2027-02-15 → 2027-02-26  
**Estado:** ⬜ Pendiente  
**Responsables:** Producto + Equipo completo  
**Dependencias:** F9

### Trabajo

- Incorporar 2–3 agencias de diseño con una muestra controlada de reglas y reservas.
- Ejecutar en paralelo con sus hojas actuales y comparar cada diferencia.
- Cargar un statement real y validar el dinero pendiente detectado.
- Corregir bloqueos de adopción y exactitud; diferir mejoras no esenciales.
- Medir tiempo de onboarding, exactitud, diferencias encontradas y uso de la explicación.
- Celebrar la revisión `go/no-go` y etiquetar el MVP v1 si se cumplen los criterios.

### Criterios de salida

- [ ] Al menos dos pilotos completan el flujo de extremo a extremo.
- [ ] El 100 % de los cálculos de la muestra aceptada coincide o tiene una diferencia explicada y aprobada.
- [ ] No se pierden ni duplican ventas, movimientos o líneas de conciliación.
- [ ] Finanzas de al menos un piloto valida el resumen esperado/cobrado/pendiente.
- [ ] Existe una decisión documentada de liberar, extender el piloto o detener el lanzamiento.

### Referencias

- [Alcance v1 · Modelo de negocio y go-to-market](./travel_commission_engine_alcance_v1.md#8-modelo-de-negocio-y-go-to-market)
- [Resumen ejecutivo · Beneficios esperados](./travel_commission_engine_mvp1_resumen_ejecutivo.md#9-beneficios-esperados)
- [Resumen ejecutivo · Conclusión](./travel_commission_engine_mvp1_resumen_ejecutivo.md#11-conclusión-y-próximos-pasos)

## Definition of Done del MVP

El MVP está completo cuando, en una misma organización piloto y sin intervención directa de desarrollo, se puede:

- [ ] Ejecutar el motor de reglas como módulo/CLI independiente, sin servicios externos y dentro de su presupuesto de rendimiento.
- [ ] Importar un Excel y publicar reglas revisadas y versionadas.
- [ ] Configurar productos simples, paquetes y contratos de proveedor.
- [ ] Simular la comisión completa antes de vender.
- [ ] Confirmar una venta idempotente y explicar cada importe por nodo.
- [ ] Consultar movimientos, saldo y estado de cuenta del agente.
- [ ] Cancelar la venta y obtener una reversión append-only exacta.
- [ ] Importar el CSV del proveedor y ver cobrado, pendiente y diferencias.
- [ ] Demostrar aislamiento entre organizaciones y ausencia de PII en el core.
- [ ] Ejecutar el flujo completo con los datos aceptados de al menos dos pilotos.

## Fuera de este roadmap

No forman parte del MVP v1 base para pilotos: `STACK`, splits a N participantes, agregación persistente, rappels retroactivos, FX automático, ledger de doble partida, pagos automáticos, matching con IA, webhooks, NATS/Kafka, Kubernetes y conciliación rica. RBAC completo y approvals maker-checker pertenecen a la variante **v1 Enterprise**, no a la ruta base F0–F10. Su destino está documentado en [Fuera de alcance v1](./travel_commission_engine_alcance_v1.md#6-fuera-de-alcance-v1-con-destino).

## Registro de seguimiento

Actualizar esta tabla en cada revisión semanal. Las decisiones de alcance deben enlazar a un issue, ADR o acta; no se incorporan silenciosamente al MVP.

| Fecha | Fase | Estado anterior | Estado nuevo | Evidencia / decisión | Responsable |
|---|---|---|---|---|---|
| 2026-08-30 | F0 | — | 🟡 En curso | Roadmap inicial creado desde la propuesta MVP | Producto / Tech Lead |
| 2026-08-30 | F1 | ⬜ Pendiente | ⬜ Pendiente, priorizada | Motor de reglas movido a la primera fase de construcción; frontera y presupuestos definidos | Tech Lead |
| 2026-08-30 | Arquitectura | — | Propuesta | Monolito modular, engine independiente, binario Go único y PostgreSQL | Tech Lead |
| 2026-08-30 | F1 | ⬜ Pendiente | ⬜ Planificada | Plan de implementación detallado enlazado | Tech Lead |
| 2026-09-04 | F1 | ⬜ Pendiente en calendario | 🟡 En curso | Calendario sincronizado con el trabajo iniciado el 2026-08-30 | Backend |
| 2026-09-04 | Verificación F1 | Verde declarado | Rojo reproducido | Build pasa; formato, tests, race y vet bloqueados por `engine_test.go`; ver corte verificable | Backend |
| 2026-09-04 | Seguridad F1 | Riesgos genéricos | Brechas concretas registradas | Fail-open de contexto, alias de outcome y límites runtime añadidos al trabajo y criterios de salida | Backend / Tech Lead |
| 2026-09-22 | F1 | Corte local desactualizado | Ranking, copia segura y T2.7 verificados localmente | Build, suite, race y vet en verde; documentación sincronizada; CI remota y cierre de F1 pendientes | Backend |
| 2026-09-22 | T2.5 | Sanitización del trace pendiente | ✅ Verificada localmente | Errores CEL sin texto interno; regresión de conversiones, división por cero, estabilidad JSON y fallback con contexto válido | Backend |
| 2026-09-22 | CLI F1/F2 | CLI separada del motor | Un único ejecutable con subcomandos | Decisión acordada: `rulefare rules validate/evaluate` en F1 y `rulefare serve` en F2; ver [decisión de CLI](./f1_motor_reglas_plan_implementacion.md#decisión-de-cli) | Producto / Tech Lead |
| 2026-09-22 | T3.1 / T3.2 | ⬜ Pendientes | ✅ Implementadas con pruebas locales | `cmd/rulefare`, validate legible/JSON y evaluate con trace; archivo de contexto, límite de 8 MiB y demostración sin servicios externos | Backend |
| 2026-09-22 | T3.3 | Pruebas concurrentes pendientes | ✅ Implementadas con evidencia local | Un mismo Program desde 16 goroutines, resultados JSON deterministas y mutación aislada de outputs; `-race` con CPU 1/4 y suite completa en verde; CI remota no verificada | Backend |
| 2026-09-22 | T3.4 | ⬜ Pendiente | ✅ Implementada y verificada localmente | Tres targets con semillas versionadas, campañas acotadas sin fallos y fuzzing de 10 s por target configurado en CI; ejecución remota no verificada | Backend |
| 2026-09-22 | T3.5 | ⬜ Pendiente | ✅ Baseline implementada y medida | Benchmarks versionados, runner local/contenedor y artefactos en CI; latencia en objetivo, RSS tras GC de 136,7 MiB pendiente de T3.6; ejecución remota no verificada | Backend |
| 2026-09-22 | T3.6 | ⬜ Pendiente | 🟡 Optimización medida; presupuesto general abierto | Reutilización CEL respaldada por perfiles; RSS 29,5 MiB en baseline, 145,3 MiB con condiciones únicas | Backend |
| 2026-09-22 | T3.6 | 🟡 Condiciones únicas sobre presupuesto | ✅ Completada localmente | RSS único mediano 86,4 MiB con 1 CPU/512 MiB; funciones CEL usadas, perfiles antes/después y equivalencia verificada | Backend |
| 2026-09-22 | T3.9 | 🟡 Límites runtime pendientes | ✅ Implementada y medida localmente | Recursos acotados y fallo sin fallback; p95/p99 y RSS en objetivo; sobrecoste >10 % queda explícito como pendiente de F1 | Backend |
| 2026-09-22 | Rendimiento T3.9 | Regresión de evaluación pendiente | ✅ Baseline original corregida con límites activos | Perfiles CPU/asignaciones, optimización de ranking/trace y reutilización local con presupuesto por regla; control único y pruebas diferenciales | Backend |
| 2026-09-22 | T3.8 | 🟡 Documentación integral pendiente | ✅ Completada localmente | API, CLI, errores y límites documentados; ejemplos Go y CLI verificados | Backend |
| 2026-09-22 | CPU en reposo / I/O | 🟡 Medición de sistema pendiente | ✅ Evidencia local conservada | Tres muestras con 10.000 condiciones únicas y 4.100 evaluaciones trazadas; CPU ≈ 0 y sin accesos de archivos/red observados | Backend |
| 2026-09-22 | T1.6 | 🟡 Solo fixtures técnicos | 🟡 Diez casos preparados para revisión | cases.json y snapshots verificables, incluidos seis niveles de jerarquía; no implica aprobación de producto/finanzas | Backend / Producto / Finanzas |
| 2026-09-22 | Preparación de CI | Medición de sistema manual | ✅ Configurada y validada localmente | CPU/I/O y artefactos incorporados, frontera de dependencias reforzada, govulncheck sin hallazgos; run del commit actual pendiente | Backend |
| 2026-09-22 | T1.6 | 🟡 Diez casos sin revisión de dominio | ✅ Corpus técnico de 32 casos revisado por IA | Revisor independiente con enfoque financiero solicitado por el responsable; 64 evaluaciones, suite/race/vet locales pasan; CI de ampliación pendiente | Backend / agente de revisión solicitado |
| 2026-09-22 | F1 | 🟡 Corpus ampliado sin CI remota | ✅ Completa técnicamente | [Run `35778045359`](https://github.com/Khr0x/rulefare/actions/runs/35778045359) de `main` para `09ff1e2` pasa; artefacto con benchmarks, CPU/I/O y corpus de 32 casos; aceptación comercial separada | Backend / Tech Lead |
