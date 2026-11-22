import { sqliteTable, text } from "drizzle-orm/sqlite-core";

export const t = sqliteTable("t", {
  // Two hints on one column.
  // @format uuid
  // @format email
  id: text("id").primaryKey(),
});
