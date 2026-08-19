-- 人気投稿のランキングを事前集計して保持する。
--
-- feed=recommended と /trending は、いいねを集計してから並べ替える必要があり、
-- リクエストのたびに likes 全体を GROUP BY していた。索引では解消できないため
-- 定期的に集計した結果をここに持ち、API は順位で引くだけにする。
--
-- window_key は集計の時間窓。'1h' が /trending、'24h' が feed=recommended。
-- rank_pos は 1 始まりの順位。主キーに含めることで
-- 「ORDER BY rank_pos LIMIT ? OFFSET ?」が索引順の範囲走査だけで済む。

CREATE TABLE IF NOT EXISTS post_ranking (
    window_key VARCHAR(8) NOT NULL,
    rank_pos   INT        NOT NULL,
    post_id    BIGINT     NOT NULL,
    score      INT        NOT NULL,
    PRIMARY KEY (window_key, rank_pos)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
