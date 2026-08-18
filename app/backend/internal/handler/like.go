package handler

import (
	"encoding/json"
	"net/http"
)

func (h *Handler) GetLikes(w http.ResponseWriter, r *http.Request) {
	postID, err := pathID(r, "id")
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid id")
		return
	}

	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT user_id FROM likes WHERE post_id = ?`, postID,
	)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			h.respondError(w, http.StatusInternalServerError, "server error")
			return
		}
		ids = append(ids, uid)
	}
	if err := rows.Err(); err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}

	fetched, err := h.fetchUsersByIDs(r, ids)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	users := make([]any, 0, len(ids))
	for _, id := range ids {
		if u, ok := fetched[id]; ok {
			users = append(users, u)
		}
	}
	h.respondJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (h *Handler) Like(w http.ResponseWriter, r *http.Request) {
	myID, _ := h.currentUserID(r)

	var req struct {
		PostID int64 `json:"post_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	h.DB.ExecContext(r.Context(),
		`INSERT INTO likes (user_id, post_id) VALUES (?, ?)
		 ON DUPLICATE KEY UPDATE user_id = user_id`,
		myID, req.PostID,
	)

	// 投稿者に通知
	var postOwnerID int64
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT user_id FROM posts WHERE id = ?`, req.PostID,
	).Scan(&postOwnerID); err == nil {
		createNotification(h, r, postOwnerID, "like", myID, &req.PostID)
	}

	var count int
	h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM likes WHERE post_id = ?`, req.PostID,
	).Scan(&count)

	h.respondJSON(w, http.StatusOK, map[string]int{"likes_count": count})
}

func (h *Handler) Unlike(w http.ResponseWriter, r *http.Request) {
	myID, _ := h.currentUserID(r)
	postID, err := pathID(r, "post_id")
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid post_id")
		return
	}

	h.DB.ExecContext(r.Context(),
		`DELETE FROM likes WHERE user_id = ? AND post_id = ?`, myID, postID,
	)

	var count int
	h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM likes WHERE post_id = ?`, postID,
	).Scan(&count)

	h.respondJSON(w, http.StatusOK, map[string]int{"likes_count": count})
}
