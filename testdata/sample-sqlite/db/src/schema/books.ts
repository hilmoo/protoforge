import { integer, real, sqliteTable, text } from "drizzle-orm/sqlite-core";
import { authors } from "./authors";
import { baseColumns } from "./shared";

export const books = sqliteTable("books", {
  ...baseColumns,
  // Book title.
  title: text("title").notNull(),
  // Owning author.
  authorId: text("author_id").notNull().references(() => authors.id),
  // Literary genre.
  genre: text("genre", { enum: ["fiction", "non-fiction", "poetry"] }).notNull(),
  // Average rating, 0 to 5.
  rating: real("rating").notNull().default(0),
  // Whether the book is in stock.
  inStock: integer("in_stock", { mode: "boolean" }).notNull(),
  // Extra attributes as JSON.
  attributes: text("attributes", { mode: "json" }),
});
