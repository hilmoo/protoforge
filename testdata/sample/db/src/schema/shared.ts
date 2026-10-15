import { timestamp, uuid } from "drizzle-orm/pg-core";

// Columns shared by every table; tables spread them with `...baseColumns`.
export const baseColumns = {
  // Unique identifier.
  // @example "4f9c2a8e-1d3b-4c5e-9f7a-2b6d8e0c1a3f"
  id: uuid("id").primaryKey().defaultRandom(),
  // When the row was created.
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  // When the row was last updated.
  updatedAt: timestamp("updated_at", { withTimezone: true }).notNull().defaultNow(),
};
