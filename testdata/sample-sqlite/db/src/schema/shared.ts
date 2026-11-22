import { integer, text } from "drizzle-orm/sqlite-core";

// Columns shared by every table; tables spread them with `...baseColumns`.
export const baseColumns = {
  // Unique identifier.
  // @format uuid
  // @example "4f9c2a8e-1d3b-4c5e-9f7a-2b6d8e0c1a3f"
  id: text("id").primaryKey().$defaultFn(() => crypto.randomUUID()),
  // When the row was created.
  createdAt: integer("created_at", { mode: "timestamp" }).notNull(),
  // When the row was last updated.
  updatedAt: integer("updated_at", { mode: "timestamp" }).notNull(),
};
