# rulefare

Rules behind every travel transaction.

> **Estado al 2026-09-04:** prototipo de la F1 (motor de reglas) en desarrollo. El motor de cálculo, API, persistencia, multi-tenancy, UI, importaciones, ledger y conciliación aún no existen. La selección de reglas sigue siendo provisional y **no debe usarse para calcular dinero en producción**.

## Estado actual

| Área | Estado verificable |
|---|---|
| Contratos, validación y compilación CEL | Implementados |
| Vigencia, scope, evaluación CEL, `NO_MATCH` y trace básico | Implementados o parciales |
| Ranking por especificidad, prioridad y empate bloqueante | Pendientes; hoy gana provisionalmente el ID alfabéticamente menor |
| CLI, fuzzing y benchmarks versionados | Pendientes |
| F2–F10 del MVP | Pendientes |
| Build del paquete | `go build ./...` pasa con Go 1.27.1 |
| Suite del árbol actual | Import de `cel` y `gofmt` de `engine/engine_test.go` corregidos; pendiente de confirmación en CI |

Ese import faltante y el formato ya están corregidos en el árbol. Una comprobación aislada previa confirmó que con esa corrección la suite existente, `go test -race` y `go vet` pasan, con 91,2 % de cobertura. Es diagnóstico local; la evidencia de CI y el criterio de salida de F1 siguen pendientes.

## Verificación local

Requiere Go 1.27 o posterior. Use la última revisión de parche disponible.

```sh
go build ./...
gofmt -l engine
go test ./...
go test -race ./...
go vet ./...
govulncheck ./...
```

Los resultados y bloqueos vigentes se registran en el [plan de F1](./propuesta/mvp/f1_motor_reglas_plan_implementacion.md#estado-verificado-al-2026-09-04).

## Estructura

```text
engine/       motor neutral en Go + CEL, sin I/O
propuesta/    alcance, roadmap y arquitectura objetivo
.github/      CI propuesta
```

## Garantías y límites actuales

- `Compile` clona las entradas, valida la estructura y precompila CEL con límites de tamaño, nodos, anidamiento y recursión.
- `Evaluate` no realiza I/O y la evaluación concurrente de solo lectura pasó una prueba diagnóstica con detector de carreras.
- Un error de tipo o una variable ausente en el contexto se trata hoy como descarte de esa regla; puede ganar una regla fallback. Falta validar y normalizar todo el contexto antes de evaluar.
- El outcome devuelto comparte memoria con el programa compilado. El consumidor no debe mutarlo; falta devolver una copia profunda.
- El trace copia hoy el texto de errores CEL; una conversión fallida puede incluir el valor de entrada. No debe exponerse sin sanitización.
- No hay todavía límite de coste de ejecución CEL ni límites completos para valores/contextos. El host debe limitar el tamaño de la entrada y no exponer esta API a reglas no confiables.
- El contrato JSON del contexto aún necesita normalización guiada por schema para enteros y timestamps.

## Documentación canónica

Cuando dos documentos discrepen, prevalece este orden:

1. [Alcance v1](./propuesta/mvp/travel_commission_engine_alcance_v1.md): qué entra en cada versión.
2. [Roadmap del MVP](./propuesta/mvp/roadmap.md): orden, dependencias y estado de entrega.
3. [Plan de implementación de F1](./propuesta/mvp/f1_motor_reglas_plan_implementacion.md): trabajo técnico del motor.
4. [Jerarquía y resolución](./propuesta/mvp/travel_commission_engine_jerarquia_reglas.md): semántica de selección.

Los documentos en `propuesta/otras-fases/` describen arquitectura objetivo y opciones futuras; no son compromisos del MVP.
