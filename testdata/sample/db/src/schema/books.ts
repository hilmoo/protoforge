import { index, integer, jsonb, numeric, pgEnum, pgTable, text, uuid } from "drizzle-orm/pg-core";
import { authors } from "./authors";
import { baseColumns } from "./shared";

export const bookGenre = pgEnum("book_genre", ["fiction", "non_fiction", "sci-fi", "poetry"]);
export const bookFormat = pgEnum("book_format", ["hardcover", "paperback", "ebook"]);

export const books = pgTable(
  "books",
  {
    ...baseColumns,
    // Author who wrote the book.
    authorId: uuid("author_id")
      .notNull()
      .references(() => authors.id, { onDelete: "cascade" }),
    // Title of the book.
    // @example "The Left Hand of Darkness"
    title: text("title").notNull(),
    // International Standard Book Number.
    isbn: text("isbn").notNull().unique(),
    genre: bookGenre("genre").notNull(), // Primary genre.
    // Available formats.
    formats: bookFormat("formats").array().notNull(),
    // Free-form tags.
    // @example #["classic", "award-winner"]
    tags: text("tags").array(),
    // Co-author IDs.
    coAuthorIds: uuid("co_author_ids").array(),
    // Number of pages.
    pages: integer("pages"),
    // List price in USD.
    // @example 12.5
    price: numeric("price", { mode: "number" }).notNull(),
    // Arbitrary publisher metadata.
    metadata: jsonb("metadata"),
  },
  (table) => [index("books_author_id_idx").on(table.authorId)],
);

export const reviews = pgTable("reviews", {
  ...baseColumns,
  // Reviewed book.
  bookId: uuid("book_id")
    .notNull()
    .references(() => books.id, { onDelete: "cascade" }),
  // Rating from 1 to 5.
  // @example 5
  rating: integer("rating").notNull(),
  body: text("body"),
});
