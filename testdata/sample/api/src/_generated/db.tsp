// AUTO-GENERATED from db/src/schema by protoforge (embedded drizzle-to-typespec).
// DO NOT EDIT. Run `protoforge stage typespec --only from_drizzle` to regenerate.

import "@typespec/openapi";
import "@typespec/openapi3";

namespace DbSchema;

@format("uuid")
scalar Uuid extends string;

enum BookFormat {
  hardcover: "hardcover",
  paperback: "paperback",
  ebook: "ebook",
}

enum BookGenre {
  fiction: "fiction",
  non_fiction: "non_fiction",
  `sci-fi`: "sci-fi",
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

  @doc("Display name of the author.")
  @example("Ursula K. Le Guin")
  name: string;

  @doc("Short biography.")
  bio?: string;

  @doc("Date of birth.")
  @example(plainDate.fromISO("1929-10-21"))
  bornOn?: plainDate;

  @doc("Whether the author is still publishing.")
  active: boolean;
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

  @doc("Author who wrote the book.")
  @format("uuid")
  @removeVisibility(Lifecycle.Update)
  authorId: string;

  @doc("Title of the book.")
  @example("The Left Hand of Darkness")
  title: string;

  @doc("International Standard Book Number.")
  isbn: string;

  @doc("Primary genre.")
  genre: BookGenre;

  @doc("Available formats.")
  formats: BookFormat[];

  @doc("Free-form tags.")
  @example(#["classic", "award-winner"])
  tags?: string[];

  @doc("Co-author IDs.")
  coAuthorIds?: Uuid[];

  @doc("Number of pages.")
  pages?: integer;

  @doc("List price in USD.")
  @example(12.5)
  price: float;

  @doc("Arbitrary publisher metadata.")
  metadata?: unknown;
}

@doc("Database table `reviews`.")
model ReviewRow {
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

  @doc("Reviewed book.")
  @format("uuid")
  @removeVisibility(Lifecycle.Update)
  bookId: string;

  @doc("Rating from 1 to 5.")
  @example(5)
  rating: integer;

  body?: string;
}
