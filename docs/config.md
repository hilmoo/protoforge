# `protoforge.yaml` reference

One file at the repo root drives the whole pipeline:

```
Drizzle schema → migrations (+ DBML docs) → TypeSpec → OpenAPI JSON → .proto
```

`protoforge` finds the file by walking up from the current directory (or use
`-c path`). Relative paths are resolved against the file's directory, and a stage's
own paths against that stage's `dir`. Forward slashes work on every OS.

Every option has a default; a section that is **absent** disables its stage, and a
section that is present but empty (`openapi:`) enables it with defaults. Unknown
keys are an error with a line number. The whole file is validated before anything
runs.

`protoforge config print` shows the resolved config, with defaults and `env:`
expansion applied.

## Top level

| key | default | meaning |
|---|---|---|
| `version` | `1` | Schema version. Must be `1`. |
| `env_file` | — | dotenv file loaded into every child process (and used for `DATABASE_URL`). It does not override variables already set in the environment. A missing file is ignored. |
| `env` | — | Map of extra variables. Values support `${VAR}` expansion against the process environment plus `env_file`. These **do** override. |
| `tools` | auto | See [tools](#tools). |
| `migration`, `typespec`, `openapi`, `proto` | absent | Stage sections, described below. |
| `pipelines` | see below | Named stage sequences. |

## `tools`

Each value is a command line, split into argv shell-style (quotes work) but never run
through a shell. All of them are optional.

| key | default resolution |
|---|---|
| `node_runner` | From the nearest lockfile: `pnpm-lock.yaml` → `pnpm exec`, `bun.lock(b)` → `bunx`, `yarn.lock` → `yarn exec`, `package-lock.json` → `npx`; otherwise `npx`. |
| `drizzle_kit`, `drizzle_docs`, `tsp`, `openapi_format`, `spectral` | npm tools. Resolution order: this override, then the nearest `node_modules/.bin` from the stage dir up to the repo root, then `PATH`, then `<node_runner> <bin>`. |
| `openapi_generator` | This override, then `openapi-generator-cli` on `PATH`. Examples: `java -jar /opt/openapi-generator-cli.jar`, `npx @openapitools/openapi-generator-cli`. |
| `ts_runner` | Runs the embedded Drizzle→TypeSpec script. The default is the first available of `tsx` (project `node_modules` or `PATH`), `bun`, or `node --experimental-strip-types` (Node ≥ 22.6). |

On Windows, `PATHEXT` is honoured, so `pnpm.cmd` and `npx.cmd` shims are found.

## `migration`

| key | default | meaning |
|---|---|---|
| `dir` | `migration` | Drizzle project directory. |
| `drizzle_config` | `drizzle.config.ts` | Passed as `--config`. |
| `drizzle_out` | `drizzle` | Drizzle's `out` directory (read by `sync`). |
| `generate` | `false` | `drizzle-kit generate`. |
| `push` | `false` | `drizzle-kit push`. |
| `migrate` | `false` | `drizzle-kit migrate`. |
| `docs` | absent | `drizzle-docs generate <schema_dir> -d <dialect> -f <format> -o <out> --force`. Keys: `schema_dir` (`./src/schema`), `dialect` (`postgresql`), `format` (`dbml`), `out` (`schema.dbml`). Use `docs: {}` for all defaults. |
| `post_push_sql` | — | List of SQL strings (each may hold several statements or `DO` blocks), run in **one transaction** over `DATABASE_URL` and rolled back on any error. Runs only after `push`, or explicitly with `--only post_push_sql`. |
| `sync` | absent | Replaces `sync.sh`. For each `<drizzle_out>/<name>/migration.sql`, writes `<to>/<name><suffix>` with backticks turned into `"` and `--> statement-breakpoint` lines removed. Keys: `to` (required, relative to `dir`), `suffix` (`.up.sql`), `extra_replace` (rules, see below). Only the *contents* of `to` are cleaned (dotfiles are kept). Refuses `/`, `$HOME`, or anything outside the repo root. |
| `run_after` | — | [Escape-hatch commands](#run_after). |

Steps: `generate` → `docs` → `push` → `post_push_sql` → `migrate` → `sync` → `run_after`
(the order of the original task pipeline).

## `typespec`

| key | default | meaning |
|---|---|---|
| `dir` | `openapi` | TypeSpec project. |
| `entry` | `./src` | Passed to `tsp compile`. If `<dir>/tspconfig.yaml` is missing, a built-in one is used (`@typespec/openapi3`, JSON, OpenAPI 3.0.0, `output-dir: {cwd}`). The emitted `@typespec/openapi3/openapi.json` is moved to `openapi.output`, and `@typespec/` is removed. |
| `from_drizzle` | absent | Runs the embedded Drizzle→TypeSpec generator. Its paths are relative to `typespec.dir`. |
| `run_after` | — | Escape hatch. |

`from_drizzle` keys:

| key | default | meaning |
|---|---|---|
| `schema_dir` | `../migration/src/schema` | Directory of Drizzle `*.ts` schema files. |
| `out` | `src/_generated/db.tsp` | Generated file. |
| `namespace` | `DbSchema` | TypeSpec namespace. |
| `read_only_columns` | `[id, created_at, updated_at]` | Columns that get `@visibility(Lifecycle.Read, Lifecycle.Delete)`. |
| `require_docs` | `warn` | Columns without a `//` doc comment: `warn` prints a warning, `error` fails without writing, `off` stays silent. |

Doc comments are read from the schema source with a small TypeScript parser, not
line regexes. A column's doc is the `//` run directly above its property (no blank
line in between), a `/** ... */` block above it, or a `//` comment at the end of its
line. Spread columns are resolved like JavaScript does it: `...base`,
`...ns.base` (namespace import), `...timestamps()` (helper returning an object) and
`...{ ... }` work across files and through aliased imports, and a later
member overrides an earlier one. Tables may be written as `pgTable("t", { ... })`,
`pgTable("t", (t) => ({ ... }))` or `schema.table("t", { ... })`. Columns are
matched by property key.

How it runs: the script is extracted to
`<project>/node_modules/.cache/protoforge/drizzle-to-tsp.<hash>.mts`, where `<project>`
is the nearest ancestor of `schema_dir` that has `node_modules/drizzle-orm`. That way
`drizzle-orm` resolves from **your** dependencies. The parameters go in as one JSON
argument, and the file is deleted afterwards (kept with `--verbose`).

Steps: `from_drizzle` → `compile` → `run_after`.

## `openapi`

| key | default | meaning |
|---|---|---|
| `dir` | `openapi` | Directory holding the document. |
| `output` | `openapi.json` | The compiled document (relative to `dir`). |
| `format` | `false` | `openapi-format --configFile oaf-config.yaml -o <output> <output>`. Without `<dir>/oaf-config.yaml`, a built-in config is used. |
| `go_tags` | `false` | Injects `x-oapi-codegen-extra-tags` (`validate` and `jsonschema` tags) in Go, byte-identical to the former `transform_openapi.py`. |
| `lint` | — | Spectral ruleset path, used only by `--only lint`. |
| `run_after` | — | Escape hatch. |

Steps: `format` → `go_tags` → `lint` (only with `--only`) → `run_after`.

## `proto`

| key | default | meaning |
|---|---|---|
| `dir` | `proto` | Working dir for `openapi-generator-cli`. |
| `registry` | `registry` | Generator output root (relative to `dir`). |
| `templates` | embedded | Directory with `api.mustache` / `model.mustache`. By default the embedded templates are used, with `models.v1.` rewritten for `proto_package`. |
| `proto_package` | `v1` | Gives `package models.<v>;` / `services.<v>;` and the output dirs `registry/{models,services}/<v>`. |
| `go_package_root` | `proto` | Gives `option go_package = "<root>/models/<v>;models<v>";` (same for services). |
| `well_known` | `{DateTime: google.protobuf.Timestamp}` | Passed as `--type-mappings` plus matching `--import-mappings` (models pass only, as before). |
| `any_type` | `google.protobuf.Any` | Replaces `AnyType` and its `any_type.proto` import. |
| `drop_imports` | `[]` | Import stems (prefix match) whose `import` lines are deleted, e.g. `todo_object_mapping`, `common_error_error`. |
| `flatten_requests` | `false` | Inlines model fields into `*Request` messages. A Go port of `flatten_services.py`. |
| `extra_replace` | — | Rules applied to `registry/{models,services}/<v>` after flattening. |
| `codegen` | absent | `{enabled: false, commands: ["buf generate"]}`, run in `dir`. |
| `run_after` | — | Escape hatch. |

Steps: `models` → `services` → `flatten` → `extra_replace` → `cleanup` (removes
`<dir>/.tmp`) → `codegen` → `run_after`.

Each pass deletes `registry/<kind>/<v>` first, then runs
`openapi-generator-cli generate -g protobuf-schema --global-property models|apis`. It
then moves the `*.proto` files into `registry/<kind>/<v>/` and applies the built-in
normalisations that replace the old `sed -i` chain.

## `pipelines`

Defaults (you can override or add to them):

```yaml
pipelines:
  api: [typespec, openapi, proto]
  sql: [migration]
  all: [sql, api]
```

Entries may name stages or other pipelines. Cycles and unknown names are rejected,
and a pipeline must not share a name with a stage. A stage whose section is absent is
skipped with a warning.

## `extra_replace` rules

```yaml
extra_replace:
  - { glob: "*.proto", from: "literal", to: "replacement" }
  - { glob: "*.proto", regex: '(\w+)_v1', to: "${1}_v2" }    # Go RE2, applied to the whole file; use (?m) for ^/$ per line
  - { glob: "services/*.proto", delete_line_matching: '^// TODO' }
```

A glob without `/` matches the base name; one with `/` matches the path relative to
the rule's base dir. Rules apply in order, and a file is rewritten only when its
content changed.

## `run_after`

```yaml
run_after:
  - buf lint                                  # argv, no shell: portable
  - { cmd: "echo done > .stamp", shell: true } # opt-in: sh -c / cmd /C
```

These are an escape hatch for edge cases, not the intended path. Plain strings are
split into argv and executed **without** a shell, so they behave the same on Windows.

## Commands and flags

```
protoforge init [--force]
protoforge run [pipeline|stage ...]            # default: all
protoforge stage <name> [--only step[,step]]   # e.g. stage openapi --only format
protoforge doctor
protoforge config print
protoforge version

global: -c/--config, --dry-run, --verbose, --no-color, --continue-on-error
```

`--dry-run` prints every command (with its working dir) and every native file
operation, and changes nothing.
