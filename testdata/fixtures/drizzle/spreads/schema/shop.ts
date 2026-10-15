import { pgSchema, pgTable, text, integer } from "drizzle-orm/pg-core";
import * as cols from "./columns";
import { timestamps as ts, withTenant, flags, note } from "./columns.js";

const shop = pgSchema("shop");

export const orders = pgTable("orders", {
  ...cols.ids(),
  ...withTenant,
  // Overrides the doc that comes from the spread.
  createdAt: cols.timestamps.createdAt,
  // Link to the order page, e.g. "https://example.com/orders/1" // not a trailing comment
  url: text("url"),

  // Separated from the column by a blank line: not attached.

  status: text("status").notNull(),
  total: integer("total") // Total in cents.
    .notNull(),
  note,
  ...flags(),
});

// The callback form and a schema-qualified table.
export const items = shop.table("items", (t) => ({
  ...ts,
  ...{
    // Inline spread.
    sku: t.text().notNull(),
  },
  id: t.uuid().primaryKey(), // Item ID.
  name: t.text().notNull(),
}));
