# Ejemplos sintéticos para F0

El [Excel de dos agencias](./f0_mock_pilotos.xlsx) reúne contratos, reservas, eventos, un statement de proveedor y una conciliación calculada. Todos los nombres, importes, porcentajes y movimientos son **inventados**. Sirve para revisar el flujo y preparar conversaciones con agencias; no acredita clientes piloto, contratos reales ni aprobación financiera. Los [criterios de salida de F0](../roadmap.md#f0--validación-y-congelamiento-del-alcance) permanecen pendientes.

## Escenarios

| Agencia ficticia | Reserva | Producto y política supuesta | Comisión esperada | Statement neto | Diferencia |
|---|---|---|---:|---:|---:|
| Horizonte Demo (HZ) | HZ-001 | Paquete: hotel MXN 2.000 al 12 %; transfer MXN 500 sin comisión. Split advisor/host 70/30. | MXN 240; advisor 168, host 72 | MXN 240 | 0 |
| Horizonte Demo (HZ) | HZ-002 | Hotel simple MXN 1.000: venta de MXN 120 y cancelación con reversión de MXN −120. | 0 neto | 0 neto (pago 120 y clawback −120) | 0 |
| Ruta Clara Demo (RC) | RC-001 | Tour MXN 8.000, tramo 7 % sobre toda la base. Split advisor/host 80/20. | MXN 560; advisor 448, host 112 | MXN 500 | **MXN 60 por cobrar** |
| Ruta Clara Demo (RC) | RC-002 | Tour MXN 5.000 en el límite inferior inclusivo del tramo 7 %. | MXN 350; advisor 280, host 70 | MXN 350 | 0 |
| Ruta Clara Demo (RC) | RC-003 | Actividad con importe fijo MXN 50 por reserva. | MXN 50; advisor 40, host 10 | MXN 50 | 0 |

En total, Horizonte espera y recibe MXN 240 netos. Ruta Clara espera MXN 960, recibe MXN 900 y conserva MXN 60 pendientes. El Excel almacena y calcula importes en **centavos enteros**: MXN 60 se representa como `6000`.

## Cómo leer el archivo

1. **Supuestos** enumera las políticas ficticias y las preguntas por resolver.
2. **Contratos** define vigencias, porcentaje hotelero, tiers de tour, importe fijo y split.
3. **Reservas** separa los componentes del paquete y su base comisionable.
4. **Cálculo** toma esos datos y calcula comisión bruta y partes advisor/host.
5. **Eventos** registra ventas y la reversión de HZ-002 con signo.
6. **Statement** simula pagos del proveedor y el clawback.
7. **Conciliación** compara esperado y recibido por la llave organización + reserva; `Split diferencia` debe ser cero.

Las tasas del tour se aplican a **toda** la base según el tramo alcanzado: menos de MXN 5.000 → 5 %; desde MXN 5.000 y antes de MXN 10.000 → 7 %; desde MXN 10.000 → 9 %. Esta modalidad es solo un supuesto de F0; [F1 devuelve el plan de tiers sin hacer cálculos](../../../engine/README.md). También son supuestos la base sin impuestos, el redondeo `HALF_UP`, la única moneda MXN por organización y la reversión total de una cancelación. No se modelan impuestos, FX, pagos al agente, devoluciones parciales ni ledger.

## Sustitución por evidencia real

Para avanzar F0, se necesitan dos agencias de diseño y, de cada una, reglas o contratos anonimizados, reservas de ejemplo y statements. El responsable debe resolver las preguntas de **Supuestos**, acordar el alcance v1 y pedir a una persona responsable de finanzas que valide los importes esperados. Los mocks pueden convertirse en plantilla de entrevista y de casos dorados; no deben marcar los criterios de F0 como completos por sí solos.
