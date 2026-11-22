import { pgTable, text } from "drizzle-orm/pg-core";

export const a = pgTable("a", {
  // Postgres table.
  name: text("name"),
});
