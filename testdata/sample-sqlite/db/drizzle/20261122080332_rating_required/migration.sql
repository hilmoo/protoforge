PRAGMA foreign_keys=OFF;--> statement-breakpoint
CREATE TABLE `__new_books` (
	`id` text PRIMARY KEY,
	`created_at` integer NOT NULL,
	`updated_at` integer NOT NULL,
	`title` text NOT NULL,
	`author_id` text NOT NULL,
	`genre` text NOT NULL,
	`rating` real DEFAULT 0 NOT NULL,
	`in_stock` integer NOT NULL,
	`attributes` text,
	CONSTRAINT `fk_books_author_id_authors_id_fk` FOREIGN KEY (`author_id`) REFERENCES `authors`(`id`)
);
--> statement-breakpoint
INSERT INTO `__new_books`(`id`, `created_at`, `updated_at`, `title`, `author_id`, `genre`, `rating`, `in_stock`, `attributes`) SELECT `id`, `created_at`, `updated_at`, `title`, `author_id`, `genre`, `rating`, `in_stock`, `attributes` FROM `books`;--> statement-breakpoint
DROP TABLE `books`;--> statement-breakpoint
ALTER TABLE `__new_books` RENAME TO `books`;--> statement-breakpoint
PRAGMA foreign_keys=ON;