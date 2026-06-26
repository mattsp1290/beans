# Suggestions

- `cmd/bn/cmd_import_test.go`: Consider adding a future full CLI export-to-import round-trip test that runs `newExportCmd` output directly through the import command path and then asserts the stored issue keeps `creation_commit`. The current tests already cover the core serialization/import boundaries, so this is non-blocking.
