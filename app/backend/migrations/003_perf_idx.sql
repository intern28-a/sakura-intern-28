-- 002 は単一列中心で ORDER BY を索引で解決できていなかったため、複合索引で補完する。
-- また reposts には二次索引が1つも無く、post_id 単体の検索が全走査になっていた。

------------------------------------------------------------------------
-- reposts: post_id 単体の検索が主キー (user_id, post_id) の左端に一致せず
-- 全索引走査になっていた。fetchPost 相当の処理でページ内の投稿ごとに実行されるため
-- 影響が最も大きい。
------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS REPOSTS_POST_ID_INDEX ON reposts (post_id);

------------------------------------------------------------------------
-- posts: タイムラインは WHERE parent_post_id IS NULL + ORDER BY created_at DESC, id DESC。
-- 並び順まで索引に含めることで filesort が消え、SELECT id がカバリングになる。
-- COUNT(*) FROM posts WHERE parent_post_id IS NULL も索引内で完結する。
------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS POSTS_PARENT_CREATED_INDEX ON posts (parent_post_id, created_at, id);

-- GET /users/{id}/posts と feed=following 用。
-- user_id を先頭にし、返信の有無での絞り込みと並び順まで含める。
CREATE INDEX IF NOT EXISTS POSTS_USER_PARENT_CREATED_INDEX ON posts (user_id, parent_post_id, created_at, id);

-- リポスト解除時の DELETE ... WHERE user_id=? AND original_post_id=? AND is_repost=TRUE 用。
CREATE INDEX IF NOT EXISTS POSTS_ORIGINAL_POST_ID_INDEX ON posts (original_post_id);

------------------------------------------------------------------------
-- notifications: 一覧は WHERE user_id=? [AND type=?] + ORDER BY created_at DESC, id DESC。
-- 既存の (user_id, is_read) は created_at を含まないため filesort になっていた。
-- 未読カウントは引き続き (user_id, is_read) が担当する。
------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS NOTIFICATIONS_USER_CREATED_INDEX ON notifications (user_id, created_at, id);
CREATE INDEX IF NOT EXISTS NOTIFICATIONS_USER_TYPE_CREATED_INDEX ON notifications (user_id, type, created_at, id);

------------------------------------------------------------------------
-- footprints: WHERE user_id=? GROUP BY visitor_id が一時表になっていた。
-- visitor_id と created_at まで含めて索引内で完結させる。
------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS FOOTPRINTS_USER_VISITOR_CREATED_INDEX ON footprints (user_id, visitor_id, created_at);

------------------------------------------------------------------------
-- 002 で作った単一列索引は、上の複合索引の左端プレフィックスに含まれるため削除する。
-- 残すと書き込みコストと領域を二重に消費する。
------------------------------------------------------------------------
DROP INDEX IF EXISTS PARENT_POST_ID_INDEX ON posts;
DROP INDEX IF EXISTS POSTS_USER_ID_INDEX ON posts;
DROP INDEX IF EXISTS FOOTPRINTS_USER_ID_INDEX ON footprints;
