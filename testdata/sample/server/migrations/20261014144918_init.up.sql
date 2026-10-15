CREATE TYPE "book_format" AS ENUM('hardcover', 'paperback', 'ebook');--> statement-breakpoint
CREATE TYPE "book_genre" AS ENUM('fiction', 'non_fiction', 'sci-fi', 'poetry');--> statement-breakpoint
CREATE TABLE "authors" (
	"id" uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	"created_at" timestamp with time zone DEFAULT now() NOT NULL,
	"updated_at" timestamp with time zone DEFAULT now() NOT NULL,
	"name" text NOT NULL,
	"bio" text,
	"born_on" date,
	"active" boolean DEFAULT true NOT NULL
);
CREATE TABLE "books" (
	"id" uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	"created_at" timestamp with time zone DEFAULT now() NOT NULL,
	"updated_at" timestamp with time zone DEFAULT now() NOT NULL,
	"author_id" uuid NOT NULL,
	"title" text NOT NULL,
	"isbn" text NOT NULL UNIQUE,
	"genre" "book_genre" NOT NULL,
	"formats" "book_format"[] NOT NULL,
	"tags" text[],
	"co_author_ids" uuid[],
	"pages" integer,
	"price" numeric NOT NULL,
	"metadata" jsonb
);
CREATE TABLE "reviews" (
	"id" uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	"created_at" timestamp with time zone DEFAULT now() NOT NULL,
	"updated_at" timestamp with time zone DEFAULT now() NOT NULL,
	"book_id" uuid NOT NULL,
	"rating" integer NOT NULL,
	"body" text
);
CREATE INDEX "books_author_id_idx" ON "books" ("author_id");--> statement-breakpoint
ALTER TABLE "books" ADD CONSTRAINT "books_author_id_authors_id_fkey" FOREIGN KEY ("author_id") REFERENCES "authors"("id") ON DELETE CASCADE;--> statement-breakpoint
ALTER TABLE "reviews" ADD CONSTRAINT "reviews_book_id_books_id_fkey" FOREIGN KEY ("book_id") REFERENCES "books"("id") ON DELETE CASCADE;