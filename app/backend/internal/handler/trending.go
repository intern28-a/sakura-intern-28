package handler

import (
	"net/http"
	"time"
)

const trendingCacheKey = "trending"

type trendRow struct {
	postID      int64
	recentLikes int64
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
		// likes を先に集約してから posts に結合する。
		// 先に結合すると like 1行ごとに posts を引くため、投稿数ではなくいいね数に比例してしまう。
		// posts への結合は、削除済み投稿のいいねを除くために残している。
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
			h.respondError(w, http.StatusInternalServerError, "server error")
			return
		}
		defer rows.Close()

		for rows.Next() {
			var t trendRow
			if err := rows.Scan(&t.postID, &t.recentLikes); err != nil {
				h.respondError(w, http.StatusInternalServerError, "server error")
				return
			}
			rawTrends = append(rawTrends, t)
		}
		if err := rows.Err(); err != nil {
			h.respondError(w, http.StatusInternalServerError, "server error")
			return
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
