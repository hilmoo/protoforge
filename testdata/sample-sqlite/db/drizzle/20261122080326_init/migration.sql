CREATE TABLE `authors` (
	`id` text PRIMARY KEY,
	`created_at` integer NOT NULL,
	`updated_at` integer NOT NULL,
	`name` text NOT NULL,
	`bio` text
);
--> statement-breakpoint
CREATE TABLE `books` (
	`id` text PRIMARY KEY,
	`created_at` integer NOT NULL,
	`updated_at` integer NOT NULL,
	`title` text NOT NULL,
	`author_id` text NOT NULL,
	`genre` text NOT NULL,
	`rating` real,
	`in_stock` integer NOT NULL,
	`attributes` text,
	CONSTRAINT `fk_books_author_id_authors_id_fk` FOREIGN KEY (`author_id`) REFERENCES `authors`(`id`)
);
