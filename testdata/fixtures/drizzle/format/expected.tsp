// AUTO-GENERATED from db/src/schema by protoforge (embedded drizzle-to-typespec).
// DO NOT EDIT. Run `protoforge stage typespec --only from_drizzle` to regenerate.

import "@typespec/openapi";
import "@typespec/openapi3";

namespace DbSchema;

@format("uuid")
scalar Uuid extends string;

@doc("Database table `contacts`.")
model ContactRow {
  @doc("Primary key; the hint repeats the type's own format, so it is emitted once.")
  @format("uuid")
  @visibility(Lifecycle.Read, Lifecycle.Delete)
  id: string;

  @doc("Mail address.")
  @format("email")
  email: string;

  @doc("Tags; a hint cannot be applied to an array column.")
  tags?: string[];

  @doc("Secondary key stored as text.")
  @example("7b1f3a5e-0000-4000-8000-000000000000")
  @format("uuid")
  ref?: string;
}
