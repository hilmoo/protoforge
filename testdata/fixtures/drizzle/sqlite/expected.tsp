// AUTO-GENERATED from db/src/schema by protoforge (embedded drizzle-to-typespec).
// DO NOT EDIT. Run `protoforge stage typespec --only from_drizzle` to regenerate.

import "@typespec/openapi";
import "@typespec/openapi3";

namespace DbSchema;

@format("uuid")
scalar Uuid extends string;

enum AuthorBookGenre {
  fiction: "fiction",
  `non-fiction`: "non-fiction",
  poetry: "poetry",
}

enum AuthorFavourite {
  fiction: "fiction",
  `non-fiction`: "non-fiction",
  poetry: "poetry",
}

@doc("Database table `author_books`.")
model AuthorBookRow {
  @doc("Primary key.")
  @format("uuid")
  @visibility(Lifecycle.Read, Lifecycle.Delete)
  id: string;

  @doc("Owning author.")
  @removeVisibility(Lifecycle.Update)
  authorId: string;

  @doc("Genre of the book; the same enum as authors.book_genre.")
  genre: AuthorBookGenre;

  @doc("Inline spread member.")
  label?: string;
}

@doc("Database table `authors`.")
model AuthorRow {
  @doc("Primary key.")
  @format("uuid")
  @visibility(Lifecycle.Read, Lifecycle.Delete)
  id: string;

  @doc("Display name.")
  name: string;

  @doc("Preferred genre.")
  favourite?: AuthorFavourite;

  @doc("Book written in the author's favourite genre.")
  bookGenre?: AuthorBookGenre;
}

@doc("Database table `kinds`.")
model KindRow {
  @doc("Primary key.")
  @format("uuid")
  @visibility(Lifecycle.Read, Lifecycle.Delete)
  id: string;

  @doc("When the row was created.")
  @visibility(Lifecycle.Read, Lifecycle.Delete)
  createdAt: utcDateTime;

  @doc("When the row was last updated, in milliseconds.")
  @visibility(Lifecycle.Read, Lifecycle.Delete)
  updatedAt: utcDateTime;

  @doc("Plain text.")
  title: string;

  @doc("Optional counter.")
  count?: integer;

  @doc("Stored as 0/1.")
  active: boolean;

  @doc("Floating point.")
  ratio?: float;

  @doc("Decimal stored as numeric.")
  price?: float;

  @doc("Decimal read as a JS number.")
  weight?: float;

  @doc("Large integer stored in a blob.")
  big?: integer;

  @doc("JSON in a text column.")
  meta?: unknown;

  @doc("JSON in a blob column.")
  extra?: unknown;

  @doc("A bare blob() is JSON in Drizzle 1.0.")
  payload?: unknown;

  @doc("Raw bytes, explicit mode.")
  thumb?: bytes;

  @doc("Plain text carrying a hint that does not match any default.")
  @format("email")
  contact?: string;

  noDoc?: string;
}
