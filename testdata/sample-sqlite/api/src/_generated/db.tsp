// AUTO-GENERATED from db/src/schema by protoforge (embedded drizzle-to-typespec).
// DO NOT EDIT. Run `protoforge stage typespec --only from_drizzle` to regenerate.

import "@typespec/openapi";
import "@typespec/openapi3";

namespace DbSchema;

@format("uuid")
scalar Uuid extends string;

enum BookGenre {
  fiction: "fiction",
  `non-fiction`: "non-fiction",
  poetry: "poetry",
}

@doc("Database table `authors`.")
model AuthorRow {
  @doc("Unique identifier.")
  @example("4f9c2a8e-1d3b-4c5e-9f7a-2b6d8e0c1a3f")
  @format("uuid")
  @visibility(Lifecycle.Read, Lifecycle.Delete)
  id: string;

  @doc("When the row was created.")
  @visibility(Lifecycle.Read, Lifecycle.Delete)
  createdAt: utcDateTime;

  @doc("When the row was last updated.")
  @visibility(Lifecycle.Read, Lifecycle.Delete)
  updatedAt: utcDateTime;

  @doc("Display name.")
  name: string;

  @doc("Free-form biography.")
  bio?: string;
}

@doc("Database table `books`.")
model BookRow {
  @doc("Unique identifier.")
  @example("4f9c2a8e-1d3b-4c5e-9f7a-2b6d8e0c1a3f")
  @format("uuid")
  @visibility(Lifecycle.Read, Lifecycle.Delete)
  id: string;

  @doc("When the row was created.")
  @visibility(Lifecycle.Read, Lifecycle.Delete)
  createdAt: utcDateTime;

  @doc("When the row was last updated.")
  @visibility(Lifecycle.Read, Lifecycle.Delete)
  updatedAt: utcDateTime;

  @doc("Book title.")
  title: string;

  @doc("Owning author.")
  @removeVisibility(Lifecycle.Update)
  authorId: string;

  @doc("Literary genre.")
  genre: BookGenre;

  @doc("Average rating, 0 to 5.")
  rating: float;

  @doc("Whether the book is in stock.")
  inStock: boolean;

  @doc("Extra attributes as JSON.")
  attributes?: unknown;
}
