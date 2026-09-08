# Migrations

Schema changes are applied by GORM's `AutoMigrate` from the struct definitions
in [`internal/models`](../internal/models), driven by
[`internal/database/migrate.go`](../internal/database/migrate.go). Run them with:

```
make db-migrate          # apply
make db-status           # show which tables exist
make db-reset            # drop everything and re-apply (local only)
```

Anything `AutoMigrate` cannot express — partial indexes, expression indexes,
extensions — lives in `postMigrationStatements()` in that same file. Those
statements must be idempotent (`CREATE INDEX IF NOT EXISTS`), because they run
on every migration pass.

## The rule that matters

**Migrations must be additive.** Deploys roll containers one at a time, and
`deploy.sh` runs migrations *before* the new image starts, so old and new code
run against the same schema for a minute or two. Adding a column or index is
safe; dropping or renaming one breaks the still-running old version.

To remove a column, split it across two releases:

1. Stop reading and writing the column; deploy.
2. Drop it; deploy.

## When to add raw SQL files here

`AutoMigrate` handles the common cases well, but it will not do data
backfills, table rewrites, or anything requiring a specific lock strategy. Put
those in this directory as timestamped SQL files and apply them deliberately —
`make db-shell` and a reviewed script beat a surprise migration during a deploy.
