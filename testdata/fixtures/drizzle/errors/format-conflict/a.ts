import { pgTable, uuid } from "drizzle-orm/pg-core";

export const t = pgTable("t", {
  // Contradicts the uuid column type.
  // @format email
  id: uuid("id").primaryKey(),
});
