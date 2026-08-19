-- post_ranking を定期的に更新する EVENT。
--
-- アプリ側で goroutine を回すとインスタンスごとに実行されてしまうため、
-- 排他のためのアドバイザリロックが必要だった。EVENT はサーバー単位で1回しか
-- 走らないので、アプリの台数に関係なく1回だけ実行される。
--
-- 前提: event_scheduler=ON （docker-compose.yml の command で指定）。
--       SUPER 権限が必要な起動時設定で、SET GLOBAL では永続化できない。
--       本番のデータベースアプライアンスでは terraform の db_parameters で設定する。
--
-- 集計の内容:
--   '1h'  … /trending 用。返信も含める。同点は post_id 降順。
--   '24h' … feed=recommended 用。返信を除く。同点は新しい投稿を優先。
-- どちらも上位 1000 件のみ保持する。これを超えるページはハンドラ側が
-- その場で集約するクエリにフォールバックする。

DROP EVENT IF EXISTS refresh_post_ranking;

DELIMITER $$

CREATE EVENT refresh_post_ranking
ON SCHEDULE EVERY 1 MINUTE
STARTS CURRENT_TIMESTAMP
ON COMPLETION PRESERVE
COMMENT '人気投稿ランキングの事前集計'
DO
BEGIN
    -- /trending: 直近1時間
    START TRANSACTION;
    DELETE FROM post_ranking WHERE window_key = '1h';
    INSERT INTO post_ranking (window_key, rank_pos, post_id, score)
    SELECT '1h', ROW_NUMBER() OVER (ORDER BY score DESC, post_id DESC), t.post_id, t.score
    FROM (
        SELECT l.post_id AS post_id, COUNT(*) AS score
        FROM likes l
        JOIN posts p ON p.id = l.post_id
        WHERE l.created_at > NOW() - INTERVAL 1 HOUR
        GROUP BY l.post_id
        ORDER BY score DESC, post_id DESC
        LIMIT 10
    ) t;
    COMMIT;

    -- feed=recommended: 直近24時間、返信を除く
    START TRANSACTION;
    DELETE FROM post_ranking WHERE window_key = '24h';
    INSERT INTO post_ranking (window_key, rank_pos, post_id, score)
    SELECT '24h', ROW_NUMBER() OVER (ORDER BY score DESC, created_at DESC, post_id DESC), t.post_id, t.score
    FROM (
        SELECT l.post_id AS post_id, COUNT(*) AS score, p.created_at AS created_at
        FROM likes l
        JOIN posts p ON p.id = l.post_id AND p.parent_post_id IS NULL
        WHERE l.created_at > NOW() - INTERVAL 24 HOUR
        GROUP BY l.post_id, p.created_at
        ORDER BY score DESC, created_at DESC, post_id DESC
        LIMIT 1000
    ) t;
    COMMIT;
END$$

DELIMITER ;
