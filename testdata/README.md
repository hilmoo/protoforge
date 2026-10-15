# testdata

- `sample/`: a small public "bookstore" project (Drizzle schema → TypeSpec →
  OpenAPI → proto) with **checked-in outputs**. The end-to-end test runs
  `protoforge run all` on a copy and fails if any file changes. Enable it with
  `npm ci` in `sample/db` and `sample/api`, plus `openapi-generator-cli` on PATH.
- `fixtures/`: per-port inputs and golden outputs (`gotags`, `flatten`, `proto/raw`,
  `syncmig`, `drizzle/spreads`, `config`).