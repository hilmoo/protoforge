import { boolean, text, timestamp, uuid } from "drizzle-orm/pg-core";

// Shared column sets, written every way a codebase tends to write them.

export const timestamps = {
  /**
   * When the row was created.
   * @example utcDateTime.fromISO("2024-01-01T00:00:00Z")
   */
  createdAt: timestamp("created_at").notNull().defaultNow(),
  updatedAt: timestamp("updated_at").notNull().defaultNow(), // When the row was last updated.
};

// Not exported: only reachable through withTenant.
const softDelete = {
  // Set when the row is soft-deleted.
  deletedAt: timestamp("deleted_at"),
};

export const withTenant = {
  ...timestamps,
  ...softDelete,
  // Owning tenant.
  tenantId: uuid("tenant_id").notNull(),
};

// Helper returning a fresh object each time.
export const ids = () => ({
  // Primary key.
  id: uuid("id").primaryKey().defaultRandom(),
});

export function flags() {
  const unused = { ignored: true };
  return {
    // Whether the row is visible to customers.
    published: boolean("published").notNull().default(false),
  };
}

/** Customer-facing note. */
export const note = text("note");
