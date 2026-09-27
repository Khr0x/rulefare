# Migraciones de PostgreSQL

Los archivos `NNNN_descripcion.sql` de este directorio se embeben en el binario y se aplican en orden numérico al arrancar `rulefare serve`.

## Reglas

- **Nombre:** cuatro dígitos o más, guion bajo y descripción en minúsculas: `0001_organization.sql`. El número es la versión; no se reutiliza.
- **Solo hacia adelante.** No hay migraciones de bajada. Para corregir un cambio publicado se añade una migración nueva.
- **Inmutables.** El migrador guarda el SHA-256 de cada archivo aplicado y se niega a arrancar si un archivo ya aplicado cambió o desapareció.
- **Una transacción por archivo.** Cada migración y su registro en `schema_migrations` se confirman juntos; si falla, no queda a medias. No uses sentencias que no admiten transacción (`CREATE INDEX CONCURRENTLY`, `VACUUM`).
- **Serializadas.** Un advisory lock de PostgreSQL impide que dos instancias migren a la vez.
- **Multi-tenant.** Toda tabla de negocio lleva `organization_id NOT NULL` (ver [plan de F2](../propuesta/mvp/f2_plataforma_plan_implementacion.md#aislamiento-multi-tenant)).
- **Dinero sin `float`.** Importes en `BIGINT` de unidades menores + `CHAR(3)` de divisa; tasas en `NUMERIC`. Nunca `real` ni `double precision`.

La tabla `schema_migrations` la crea el propio migrador; no es una migración.
