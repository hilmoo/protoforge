import { boolean, date, pgTable, text } from "drizzle-orm/pg-core";
import { baseColumns } from "./shared";

export const authors = pgTable("authors", {
  ...baseColumns,
  // Display name of the author.
  // @example "Ursula K. Le Guin"
  name: text("name").notNull(),
  // Short biography.
  bio: text("bio"),
  // Date of birth.
  // @example plainDate.fromISO("1929-10-21")
  bornOn: date("born_on"),
  // Whether the author is still publishing.
  active: boolean("active").notNull().default(true),
});
