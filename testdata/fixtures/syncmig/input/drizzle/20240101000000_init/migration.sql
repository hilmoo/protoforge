CREATE TABLE `users` (
	`id` uuid PRIMARY KEY
);
--> statement-breakpoint
CREATE INDEX `idx` ON `users` (`id`);
-->   statement-breakpoint
 --> statement-breakpoint (indented: kept)
ALTER TABLE `users` ADD `x` text;
--> statement-breakpoint