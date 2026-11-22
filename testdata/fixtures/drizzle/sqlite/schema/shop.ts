import { blob, integer, numeric, real, sqliteTable, text } from "drizzle-orm/sqlite-core";
import { ids, timestamps } from "./columns";

// Every SQLite column kind, one per column.
export const kinds = sqliteTable("kinds", {
  ...ids(),
  ...timestamps,
  // Plain text.
  title: text("title").notNull(),
  // Optional counter.
  count: integer("count"),
  // Stored as 0/1.
  active: integer("active", { mode: "boolean" }).notNull(),
  // Floating point.
  ratio: real("ratio"),
  // Decimal stored as numeric.
  price: numeric("price"),
  // Decimal read as a JS number.
  weight: numeric("weight", { mode: "number" }),
  // Large integer stored in a blob.
  big: blob("big", { mode: "bigint" }),
  // JSON in a text column.
  meta: text("meta", { mode: "json" }),
  // JSON in a blob column.
  extra: blob("extra", { mode: "json" }),
  // A bare blob() is JSON in Drizzle 1.0.
  payload: blob("payload"),
  // Raw bytes, explicit mode.
  thumb: blob("thumb", { mode: "buffer" }),
  // Plain text carrying a hint that does not match any default.
  // @format email
  contact: text("contact"),
  no_doc: text("no_doc"),
});

// A text enum, a foreign key and an inline spread.
export const authors = sqliteTable("authors", {
  ...ids(),
  // Display name.
  name: text("name").notNull(),
  // Preferred genre.
  favourite: text("favourite", { enum: ["fiction", "non-fiction", "poetry"] }),
  // Book written in the author's favourite genre.
  book_genre: text("book_genre", { enum: ["fiction", "non-fiction", "poetry"] }),
});

export const authorBooks = sqliteTable("author_books", {
  ...ids(),
  // Owning author.
  authorId: text("author_id")
    .notNull()
    .references(() => authors.id),
  // Genre of the book; the same enum as authors.book_genre.
  genre: text("genre", { enum: ["fiction", "non-fiction", "poetry"] }).notNull(),
  // Free-form label.
  ...{
    // Inline spread member.
    label: text("label"),
  },
});
