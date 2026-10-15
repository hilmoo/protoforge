// AUTO-GENERATED from db/src/schema by protoforge (embedded drizzle-to-typespec).
// DO NOT EDIT. Run `protoforge stage typespec --only from_drizzle` to regenerate.

import "@typespec/openapi";
import "@typespec/openapi3";

namespace DbSchema;

@format("uuid")
scalar Uuid extends string;

@doc("Database table `items`.")
model ItemRow {
  @doc("When the row was created.")
  @example(utcDateTime.fromISO("2024-01-01T00:00:00Z"))
  @visibility(Lifecycle.Read, Lifecycle.Delete)
  createdAt: utcDateTime;

  @doc("When the row was last updated.")
  @visibility(Lifecycle.Read, Lifecycle.Delete)
  updatedAt: utcDateTime;

  @doc("Inline spread.")
  sku: string;

  @doc("Item ID.")
  @format("uuid")
  @visibility(Lifecycle.Read, Lifecycle.Delete)
  id: string;

  name: string;
}

@doc("Database table `orders`.")
model OrderRow {
  @doc("Primary key.")
  @format("uuid")
  @visibility(Lifecycle.Read, Lifecycle.Delete)
  id: string;

  @doc("Overrides the doc that comes from the spread.")
  @visibility(Lifecycle.Read, Lifecycle.Delete)
  createdAt: utcDateTime;

  @doc("When the row was last updated.")
  @visibility(Lifecycle.Read, Lifecycle.Delete)
  updatedAt: utcDateTime;

  @doc("Set when the row is soft-deleted.")
  deletedAt?: utcDateTime;

  @doc("Owning tenant.")
  @format("uuid")
  tenantId: string;

  @doc("Link to the order page, e.g. \"https://example.com/orders/1\" // not a trailing comment")
  url?: string;

  status: string;

  @doc("Total in cents.")
  total: integer;

  @doc("Customer-facing note.")
  note?: string;

  @doc("Whether the row is visible to customers.")
  published: boolean;
}
