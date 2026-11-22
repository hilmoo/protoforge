import { integer, text } from "drizzle-orm/sqlite-core";

// Shared column set, spread into several tables.
export const timestamps = {
  // When the row was created.
  createdAt: integer("created_at", { mode: "timestamp" }).notNull(),
  // When the row was last updated, in milliseconds.
  updatedAt: integer("updated_at", { mode: "timestamp_ms" }).notNull(),
};

export const ids = () => ({
  // Primary key.
  // @format uuid
  id: text("id").primaryKey(),
});
