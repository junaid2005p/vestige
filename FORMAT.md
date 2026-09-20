# Repository format and migration policy

Vestige currently writes repository format **v1**. `config.json` and every
published snapshot manifest carry `format_version: 1`. A snapshot manifest is
the commit record: chunks written without a valid manifest are not part of a
backup and may be removed by `vestige gc`.

## Compatibility contract

- A Vestige release reads every format version explicitly listed as supported
  by that release. It never guesses how to interpret an unknown version.
- Writers only create the newest format they understand. A format upgrade is
  explicit and must be atomic: write new objects first, validate them, then
  publish the new configuration version last.
- Downgrades are not supported. Back up or replicate a repository before a
  future migration.
- Format v1 has no migration path because it is the initial public format.
  Opening any version other than v1 fails safely without modifying data.

Before and after any future migration, run `vestige check` and `vestige verify`.
Migration code must preserve snapshot IDs and chunk IDs, be idempotent after an
interruption, and include local and S3 integration tests.
