-- Cleanup existing orphan rows before adding foreign keys
DELETE l FROM likes l LEFT JOIN posts p ON l.post_id = p.id WHERE p.id IS NULL;
DELETE r FROM reposts r LEFT JOIN posts p ON r.post_id = p.id WHERE p.id IS NULL;
UPDATE notifications n LEFT JOIN posts p ON n.post_id = p.id
SET n.post_id = NULL
WHERE n.post_id IS NOT NULL AND p.id IS NULL;
DELETE p FROM posts p LEFT JOIN posts op ON p.original_post_id = op.id
WHERE p.original_post_id IS NOT NULL AND op.id IS NULL;
DELETE p FROM posts p LEFT JOIN posts pp ON p.parent_post_id = pp.id
WHERE p.parent_post_id IS NOT NULL AND pp.id IS NULL;

ALTER TABLE sessions
    ADD CONSTRAINT fk_sessions_user
    FOREIGN KEY (user_id) REFERENCES users (id);

ALTER TABLE posts
	ADD CONSTRAINT fk_posts_user
	FOREIGN KEY (user_id) REFERENCES users (id),
	ADD CONSTRAINT fk_posts_original_post
	FOREIGN KEY (original_post_id) REFERENCES posts (id)
	ON DELETE CASCADE,
	ADD CONSTRAINT fk_posts_parent_post
	FOREIGN KEY (parent_post_id) REFERENCES posts (id)
	ON DELETE CASCADE;

ALTER TABLE follows
	ADD CONSTRAINT fk_follows_follower
	FOREIGN KEY (follower_id) REFERENCES users (id),
	ADD CONSTRAINT fk_follows_followee
	FOREIGN KEY (followee_id) REFERENCES users (id);

ALTER TABLE likes
	ADD CONSTRAINT fk_likes_user
	FOREIGN KEY (user_id) REFERENCES users (id),
	ADD CONSTRAINT fk_likes_post
	FOREIGN KEY (post_id) REFERENCES posts (id)
	ON DELETE CASCADE;

ALTER TABLE reposts
	ADD CONSTRAINT fk_reposts_user
	FOREIGN KEY (user_id) REFERENCES users (id),
	ADD CONSTRAINT fk_reposts_post
	FOREIGN KEY (post_id) REFERENCES posts (id)
	ON DELETE CASCADE;

ALTER TABLE footprints
	ADD CONSTRAINT fk_footprints_user
	FOREIGN KEY (user_id) REFERENCES users (id),
	ADD CONSTRAINT fk_footprints_visitor
	FOREIGN KEY (visitor_id) REFERENCES users (id);

ALTER TABLE notifications
	ADD CONSTRAINT fk_notifications_user
	FOREIGN KEY (user_id) REFERENCES users (id),
	ADD CONSTRAINT fk_notifications_actor
	FOREIGN KEY (actor_id) REFERENCES users (id),
	ADD CONSTRAINT fk_notifications_post
	FOREIGN KEY (post_id) REFERENCES posts (id)
	ON DELETE SET NULL;
