import { pgTable, text, uuid } from "drizzle-orm/pg-core";

export const contacts = pgTable("contacts", {
  // Primary key; the hint repeats the type's own format, so it is emitted once.
  // @format uuid
  id: uuid("id").primaryKey().defaultRandom(),
  // Mail address.
  // @format email
  email: text("email").notNull(),
  // Tags; a hint cannot be applied to an array column.
  // @format hostname
  tags: text("tags").array(),
  // Secondary key stored as text.
  // @example "7b1f3a5e-0000-4000-8000-000000000000"
  // @format uuid
  ref: text("ref"),
});
