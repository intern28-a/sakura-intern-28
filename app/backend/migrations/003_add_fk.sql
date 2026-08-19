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
