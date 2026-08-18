package handler

import (
	"net/http"
)

func (h *Handler) GetTrending(w http.ResponseWriter, r *http.Request) {
	myID, _ := h.currentUserID(r)

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT l.post_id, COUNT(*) AS recent_likes
		FROM likes l
		WHERE l.created_at >= NOW() - INTERVAL 1 HOUR
		GROUP BY l.post_id
		ORDER BY recent_likes DESC, l.post_id DESC
		LIMIT 20
	`)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	defer rows.Close()

	type trendRow struct {
		postID      int64
		recentLikes int64
	}
	var rawTrends []trendRow
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
