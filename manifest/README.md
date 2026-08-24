# manifest/

Check definitions. Probes are POSIX sh strings; the runner never interprets them.

The field-by-field contract is `docs/design/MANIFEST_SCHEMA.md`. `_schema.json`
is the mechanical validation of it, and runs before any probe executes.
