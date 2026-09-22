# Verificación técnica de F1 y preparación de CI

Se verificaron criterios técnicos del árbol local y se incorporó la medición de CPU/I/O al workflow. **F1 permanece abierta:** faltan aprobación de producto/finanzas del corpus y evidencia remota del commit de entrega, incluidos sus artefactos de rendimiento.

## Evidencia local

| Criterio | Resultado / evidencia |
|---|---|
| Dependencia directa del paquete | Solo librería estándar y paquetes de `github.com/google/cel-go`; CEL conserva sus dependencias transitivas de go.mod/go.sum. |
| Independencia de plataforma | Ningún import directo del proyecto o de infraestructura. El control rechaza un import externo inyectado y un fallo simulado de `go list`: [resultados](dependency-check.txt). |
| Ranking, prioridades, empate, vigencia y NO_MATCH | Pruebas dedicadas existentes y diez casos dorados pasan: [ejecución](acceptance-tests.txt). Es conformidad técnica, no aprobación comercial. |
| Outcomes sin float | `TestContractJSONRoundTrip` preserva el ruleset con percentage, fixed y tiered y vuelve a compilarlo; los snapshots dorados verifican los tres outcomes devueltos. |
| Vulnerabilidades | `govulncheck@v1.7.0 ./...`, Go 1.27.1, consulta a `https://vuln.go.dev`: [sin vulnerabilidades detectadas](govulncheck.txt). Resultado puntual, no garantía permanente. |
| Sintaxis de CI | `actionlint 1.7.7` sin errores. `git diff --check` sin errores. |
| Alcance F1 | Fuentes de producción limitadas a `engine` y `cmd/rulefare`: resolución y CLI de archivos. Sin servidor HTTP, persistencia, cálculo monetario, multi-tenancy ni STACK. |

[Entorno y hashes](environment.txt) identifican las fuentes y fixtures observados. No se añadió lógica de producción en esta revisión.

## Ajustes de CI

- `workflow_dispatch` permite ejecutar el workflow manualmente una vez publicado.
- El job tiene un límite de 20 minutos.
- La comprobación de imports admite únicamente paquetes estándar o CEL. Un fallo de `go list` aborta el paso, sin quedar oculto por `|| true`.
- Tras los benchmarks se ejecuta `scripts/measure-system.sh bin/system`: tres ventanas de reposo y trazado de I/O con controles positivos. La CPU se informa sin umbral automático; el runner rechaza I/O inesperado.
- El artefacto `engine-benchmarks-${{ github.sha }}` conserva los `.txt` de benchmarks y sistema, incluyendo el registro completo de syscalls. `if: always()` permite conservar evidencia parcial ante fallos.
- Se mantienen suite, detector de carreras, fuzzing y escaneo de vulnerabilidades. Las tres Actions referencian SHA completos, cuya existencia se verificó en sus repositorios oficiales: [checkout](https://github.com/actions/checkout/commit/d23441a48e516b6c34aea4fa41551a30e30af803), [setup-go](https://github.com/actions/setup-go/commit/b7ad1dad31e06c5925ef5d2fc7ad053ef454303e) y [upload-artifact](https://github.com/actions/upload-artifact/commit/ea165f8d65b6e75b540449e92b4886f43607fa02).

El runner de sistema ya pasó localmente en Linux arm64. El runner de GitHub puede usar otra arquitectura: sus mediciones forman una baseline propia, no una comparación directa con Apple M4/Linux arm64. Las descargas necesarias para preparar herramientas/imágenes requieren red; los contenedores medidos se ejecutan sin red.

## Evidencia remota encontrada

La consulta de GitHub Actions confirmó el [run 34059022380](https://github.com/Khr0x/rulefare/actions/runs/34059022380), disparado por `push` el **2026-09-06**, con resultado **success** para `6c86f4df4dbcbc023834ddb9ac97440de17e6dda`.

El job `test` registra success en formato, módulo, frontera de imports, vet, suite, race y escaneo de vulnerabilidades. **Ese run no contiene los cambios locales posteriores**: no acredita el corpus actual, fuzzing, benchmarks ni las mediciones de sistema añadidas al workflow. Consultar solo los runs de pull request no lo encontraba; se verificaron también los disparados por push.

La evidencia de entrega debe proceder de un nuevo run del commit que incluya los cambios actuales. Registrar su SHA, URL, resultado y artefactos cuando exista; no reutilizar el run antiguo como cierre de F1. Esta revisión no publica ni crea un commit.
