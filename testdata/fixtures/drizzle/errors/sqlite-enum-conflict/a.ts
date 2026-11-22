import { sqliteTable, text } from "drizzle-orm/sqlite-core";

// Both columns produce the enum name AuthorBookGenre with different values.
export const authors = sqliteTable("authors", {
  // First.
  bookGenre: text("book_genre", { enum: ["a", "b"] }),
});

export const authorBooks = sqliteTable("author_books", {
  // Second.
  genre: text("genre", { enum: ["a", "c"] }),
});
