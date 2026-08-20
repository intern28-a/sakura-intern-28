package handler

import (
	"database/sql"
	"net/http"
	"time"

	"sakuravel/internal/ranking"
)

const trendingCacheKey = "trending"

type trendRow struct {
	postID      int64
	recentLikes int64
}

// trendingFromRanking は事前集計済みのランキングを順位で引く。
func (h *Handler) trendingFromRanking(r *http.Request) ([]trendRow, error) {
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT post_id, score FROM post_ranking
		WHERE window_key = ?
		ORDER BY rank_pos
		LIMIT 20
	`, ranking.Window1h)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTrendRows(rows)
}

// trendingLive はランキングが未生成のときに、その場でいいねを集約する。
// 先に結合すると like 1行ごとに posts を引くため、集約を先に行う。
// posts への結合は、削除済み投稿のいいねを除くために残している。
func (h *Handler) trendingLive(r *http.Request) ([]trendRow, error) {
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT t.post_id, t.recent_likes
		FROM (
			SELECT post_id, COUNT(*) AS recent_likes
			FROM likes
			WHERE created_at >= NOW() - INTERVAL 1 HOUR
			GROUP BY post_id
		) t
		JOIN posts p ON p.id = t.post_id
		ORDER BY t.recent_likes DESC, t.post_id DESC
		LIMIT 20
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTrendRows(rows)
}

func scanTrendRows(rows *sql.Rows) ([]trendRow, error) {
	var out []trendRow
	for rows.Next() {
		var t trendRow
		if err := rows.Scan(&t.postID, &t.recentLikes); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (h *Handler) GetTrending(w http.ResponseWriter, r *http.Request) {
	myID, _ := h.currentUserID(r)

	var rawTrends []trendRow
	if h.TrendingCache != nil {
		if cached, ok := h.TrendingCache.Get(trendingCacheKey); ok {
			if v, ok := cached.([]trendRow); ok {
				rawTrends = v
			}
		}
	}

	if rawTrends == nil {
		// いいねの集約は EVENT が定期的に post_ranking へ書き出している。
		var err error
		rawTrends, err = h.trendingFromRanking(r)
		if err != nil {
			h.respondError(w, http.StatusInternalServerError, "server error")
			return
		}
		// EVENT がまだ走っていないときは、その場で集約する。
		if len(rawTrends) == 0 {
			rawTrends, err = h.trendingLive(r)
			if err != nil {
				h.respondError(w, http.StatusInternalServerError, "server error")
				return
			}
		}
		if h.TrendingCache != nil {
			h.TrendingCache.Set(trendingCacheKey, rawTrends, 30*time.Second)
		}
	}

	ids := make([]int64, 0, len(rawTrends))
	for _, rt := range rawTrends {
		ids = append(ids, rt.postID)
	}
	fetched, err := h.fetchPostsByIDs(r, ids, myID)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}

	posts := make([]any, 0, len(rawTrends))
	for _, rt := range rawTrends {
		p, ok := fetched[rt.postID]
		if !ok {
			continue
		}
		posts = append(posts, map[string]any{
			"post":         p,
			"recent_likes": int(rt.recentLikes),
		})
	}

	h.respondJSON(w, http.StatusOK, map[string]any{
		"trending": posts,
	})
}
