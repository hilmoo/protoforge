# protoforge

Drizzle → TypeSpec → OpenAPI → `.proto`, driven by a single `protoforge.yaml`.

protoforge replaces a pipeline that was glued together with Taskfiles, `sed -i`
chains and helper scripts. It is one static Go binary that orchestrates the tools
you already use and does the text-munging steps natively, so it behaves the same on
Linux, macOS and Windows.

```
Drizzle schema ─► migrations (+ DBML docs) ─► TypeSpec ─► OpenAPI JSON ─► .proto ─► (buf generate)
   migration            migration              typespec      openapi         proto        codegen
```

## Stages

| Stage | What it does | Wraps |
|---|---|---|
| `migration` | `drizzle-kit generate/push/migrate`, DBML/markdown docs, transactional `post_push_sql`, and `sync` of migration SQL into another tree | `drizzle-kit`, `drizzle-docs`, Postgres (`pgx`) |
| `typespec` | Generates TypeSpec models/enums from your Drizzle schema (embedded script), then compiles | `tsp`, `tsx`/`bun`/`node` |
| `openapi` | Formats the document and injects Go struct tags (`validate`, `jsonschema`) via `x-oapi-codegen-extra-tags` | `openapi-format`, `spectral` (optional lint) |
| `proto` | Runs the `protobuf-schema` generator for models and services, normalises output, flattens `*Request` messages, applies custom rewrites, optional `buf generate` | `openapi-generator-cli`, `buf` (optional) |

Pipelines group stages. Built in: `api` = typespec → openapi → proto, `sql` =
migration, `all` = sql → api. Define your own in `pipelines:`.

## Install

Download a binary from the Releases page (Linux/macOS/Windows, amd64/arm64), or
build from source:

```sh
go install github.com/hilmoo/protoforge/cmd/protoforge@latest
```

### External tools

protoforge only needs the tools for the stages you enable. Run `protoforge init`
to see the suggested install line, then check with `protoforge doctor`:

```sh
pnpm add -D drizzle-kit drizzle-docs-generator tsx \
  @typespec/compiler @typespec/http @typespec/openapi @typespec/openapi3 @typespec/rest \
  openapi-format
pnpm add drizzle-orm
```

The `proto` stage also needs `openapi-generator-cli` (Java). Optional: `spectral`,
`buf`.

## Quick start

```sh
protoforge init          # writes protoforge.yaml, tspconfig.yaml, oaf-config.yaml; updates .gitignore
protoforge doctor        # verify the external tools for enabled stages
protoforge run --dry-run # print every command and file operation, change nothing
protoforge run           # run everything (the `all` pipeline)
```

`init` detects an existing `drizzle.config.ts`, `main.tsp` and `openapi.json` (up
to three levels deep) and fills in the paths. It never overwrites without `--force`.

## Usage

```
protoforge init [--force]
protoforge run [pipeline|stage|stage.step ...]   # default: all
protoforge stage <name> [--only step[,step]]     # run one stage, or selected steps
protoforge doctor                                # check external tools
protoforge config print                          # resolved config (defaults + env applied)
protoforge config schema [-o file]               # JSON Schema for editor validation
protoforge version

global flags: -c/--config <path>, --dry-run, --verbose, --no-color, --continue-on-error
```

Examples:

```sh
protoforge run api                         # typespec → openapi → proto
protoforge run typespec openapi            # stages and pipelines can be mixed
protoforge run typespec.from_drizzle       # Drizzle → TypeSpec only
protoforge run migration.docs              # drizzle docs only
protoforge run typespec.compile            # TypeSpec → OpenAPI only
protoforge stage openapi --only format     # a single step
protoforge stage migration --only post_push_sql
protoforge run all --continue-on-error     # report every failure at the end
protoforge run api --spec billing          # only one service (see specs)
```

Steps per stage (the order they run in):

| Stage | Steps |
|---|---|
| `migration` | `generate` `docs` `push` `post_push_sql` `migrate` `sync` `run_after` |
| `typespec` | `from_drizzle` `compile` `run_after` |
| `openapi` | `format` `go_tags` `lint` (only with `--only`) `run_after` |
| `proto` | `models` `services` `flatten` `extra_replace` `cleanup` `codegen` `run_after` |

protoforge finds `protoforge.yaml` by walking up from the current directory. A stage
whose section is absent is skipped with a warning during `run`.

## Configuration

Everything lives in one file. The repository's own [`protoforge.yaml`](protoforge.yaml)
is a fully commented example covering **every** option; the complete reference with
defaults is in [`docs/config.md`](docs/config.md).

A minimal config:

```yaml
# yaml-language-server: $schema=./schema/protoforge.schema.json
version: 1
env_file: .env

migration:
  generate: true

typespec:
  from_drizzle: {}

openapi:
  format: true

proto:
  flatten_requests: true
```

Key rules:

- **Absent section = stage disabled; empty section (`openapi:`) = enabled with defaults.**
- Several services: list them under `specs:` (one TypeSpec entry → one OpenAPI
  document → `registry/services/<name>/<v>` each, sharing one models package). Without
  `specs`, there is one spec `main` compiled from `./src` to `openapi.json`.
- Relative paths resolve against the config file's directory; a stage's own paths
  against that stage's `dir`. Forward slashes work everywhere.
- Unknown keys are errors with line numbers, and the whole file is validated before
  anything runs.
- `env_file` is loaded into every child process and used for `DATABASE_URL`; `env:`
  values support `${VAR}` and override the environment.
- Commands (`run_after`, `codegen.commands`) are plain strings run as argv **without a
  shell**, or `{cmd, shell: true}` as an opt-in escape hatch.
- `extra_replace` rules take a `glob` plus exactly one of `from`, `regex` or
  `delete_line_matching`.

### Editor validation

Generate the JSON Schema and point your editor's YAML language server at it:

```sh
protoforge config schema -o schema/protoforge.schema.json
```

```yaml
# yaml-language-server: $schema=./schema/protoforge.schema.json
```

### Drizzle → TypeSpec doc comments

Column doc comments are read from your schema source (`//` above the column,
`/** */`, or a trailing `//`), including columns that come from spreads such as
`...timestamps()`. Control missing docs with `typespec.from_drizzle.require_docs`
(`warn` | `error` | `off`).

## Development

```sh
go build ./cmd/protoforge
go test ./...
```

Layout:

| Path | Purpose |
|---|---|
| `cmd/protoforge` | CLI entry point (cobra) |
| `internal/config` | Config types, defaults, validation, JSON Schema generation |
| `internal/stages` | The four stages and their steps |
| `internal/runner` | Process execution, tool resolution, dry-run, env handling |
| `internal/flatten`, `gotags`, `syncmig`, `textops`, `pgsql` | Native replacements for the old scripts |
| `internal/assets` | Embedded Drizzle→TypeSpec script, mustache templates, default tool configs |
| `testdata/` | Golden fixtures and a sample project |

