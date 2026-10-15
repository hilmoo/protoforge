CREATE TABLE "users" (
	"id" uuid PRIMARY KEY
);
CREATE INDEX "idx" ON "users" ("id");
 --> statement-breakpoint (indented: kept)
ALTER TABLE "users" ADD "x" text;
