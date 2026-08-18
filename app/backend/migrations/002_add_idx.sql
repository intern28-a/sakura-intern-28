-- users.emailはUNIQUE制約によってインデックスが作成されているので貼らない

CREATE INDEX PARENT_POST_ID_INDEX ON posts (parent_post_id);
CREATE INDEX POSTS_USER_ID_INDEX ON posts (user_id);

-- 単体のインデックスは複合インデックスの左端一致で代用できるためつけない
CREATE INDEX FOLLOWS_FOLLOWEE_FOLLOWER_ID_INDEX ON follows (followee_id, follower_id);

-- likes.user_id, likes.post_id は (user_id, post_id) の複合主キーだが、post_id 単体検索があるためインデックスを貼る
CREATE INDEX LIKES_POST_ID_USER_ID_INDEX ON likes (post_id, user_id);
-- トレンドは「直近1時間で集計」するので created_at を先頭にした複合インデックスが重要
CREATE INDEX LIKES_CREATED_AT_POST_ID_INDEX ON likes (created_at, post_id);

CREATE INDEX FOOTPRINTS_USER_ID_INDEX ON footprints (user_id);

-- notifications.user_id の単体インデックスは複合インデックスで代用可能なので貼らない
CREATE INDEX NOTIFICATIONS_USER_ID_IS_READ_INDEX ON notifications (user_id, is_read);

-- posts.content, users.username, users.display_nameは、FULLTEXT INDEXを貼るとテーブル自体がかなり重くなると思ったので貼らなかった
