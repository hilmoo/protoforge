import { sqliteTable, text } from "drizzle-orm/sqlite-core";

export const b = sqliteTable("b", {
  // SQLite table.
  name: text("name"),
});
