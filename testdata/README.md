# testdata

- `sample/`: a small public "bookstore" project (Drizzle schema → TypeSpec →
  OpenAPI → proto) with **checked-in outputs**. The end-to-end test runs
  `protoforge run all` on a copy and fails if any file changes. Enable it with
  `npm ci` in `sample/db` and `sample/api`, plus `openapi-generator-cli` on PATH.
- `sample-sqlite/`: the same idea for a `sqlite-core` schema (`migration.dialect: sqlite`).
  The test runs the migration stage (generate, docs, sync) and
  `typespec.from_drizzle` on a copy and fails on drift. `drizzle-kit push` is not run
  (it needs a native SQLite driver package). Enable it with `npm ci` in `sample-sqlite/db`.
- `fixtures/`: per-port inputs and golden outputs (`gotags`, `flatten`, `proto/raw`,
  `syncmig` (incl. `syncmig/sqlite`: real drizzle-kit SQLite output), `drizzle/spreads`,
  `drizzle/sqlite`, `drizzle/format`, `drizzle/errors`, `config`).