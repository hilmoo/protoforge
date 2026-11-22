import { sqliteTable, text } from "drizzle-orm/sqlite-core";
import { baseColumns } from "./shared";

export const authors = sqliteTable("authors", {
  ...baseColumns,
  // Display name.
  name: text("name").notNull(),
  // Free-form biography.
  bio: text("bio"),
});
