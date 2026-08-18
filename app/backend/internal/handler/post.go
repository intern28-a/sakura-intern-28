package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

func (h *Handler) GetTimeline(w http.ResponseWriter, r *http.Request) {
	myID, _ := h.currentUserID(r)
	page, perPage, offset := h.pagination(r)
	feed := r.URL.Query().Get("feed")

	var rows *sql.Rows
	var err error

	// 返信（parent_post_id あり）はスレッド画面でのみ表示するためタイムラインから除く
	switch feed {
	// 一覧クエリでは ID だけを引き、本体はまとめて取得する。
	// posts(parent_post_id, created_at, id) のインデックスがあればカバリングインデックスになる。
	case "latest":
		rows, err = h.DB.QueryContext(r.Context(), `
			SELECT id
			FROM posts
			WHERE parent_post_id IS NULL
			ORDER BY created_at DESC, id DESC
			LIMIT ? OFFSET ?
		`, perPage, offset)
	case "recommended":
		rows, err = h.DB.QueryContext(r.Context(), `
			SELECT p.id
			FROM posts p
			LEFT JOIN likes l ON l.post_id = p.id AND l.created_at > NOW() - INTERVAL 24 HOUR
			WHERE p.parent_post_id IS NULL
			GROUP BY p.id, p.user_id, p.content, p.is_repost, p.original_post_id, p.created_at
			ORDER BY COUNT(l.post_id) DESC, p.created_at DESC, p.id DESC
			LIMIT ? OFFSET ?
		`, perPage, offset)
	default: // "following"
		rows, err = h.DB.QueryContext(r.Context(), `
			SELECT id
			FROM posts
			WHERE parent_post_id IS NULL
			  AND user_id IN (
				SELECT followee_id FROM follows WHERE follower_id = ?
			)
			ORDER BY created_at DESC, id DESC
			LIMIT ? OFFSET ?
		`, myID, perPage, offset)
	}
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			h.respondError(w, http.StatusInternalServerError, "server error")
			return
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}

	fetched, err := h.fetchPostsByIDs(r, ids, myID)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	posts := make([]any, 0, len(ids))
	for _, id := range ids {
		if p, ok := fetched[id]; ok {
			posts = append(posts, p)
		}
	}

	var total int
	switch feed {
	case "latest", "recommended":
		h.DB.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM posts WHERE parent_post_id IS NULL`,
		).Scan(&total)
	default: // "following"
		h.DB.QueryRowContext(r.Context(), `
			SELECT COUNT(*) FROM posts
			WHERE parent_post_id IS NULL
			  AND user_id IN (
				SELECT followee_id FROM follows WHERE follower_id = ?
			)
		`, myID).Scan(&total)
	}

	h.respondJSON(w, http.StatusOK, map[string]any{
		"posts":    posts,
		"total":    total,
		"page":     page,
		"per_page": perPage,
	})
}

func (h *Handler) CreatePost(w http.ResponseWriter, r *http.Request) {
	myID, _ := h.currentUserID(r)

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Content == "" || len([]rune(req.Content)) > 140 {
		h.respondError(w, http.StatusBadRequest, "content must be 1-140 characters")
		return
	}

	res, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO posts (user_id, content) VALUES (?, ?)`,
		myID, req.Content,
	)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	postID, err := res.LastInsertId()
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}

	post, _ := h.fetchPost(r, postID, myID)
	h.respondJSON(w, http.StatusCreated, map[string]any{"post": post})
}

func (h *Handler) GetUserPosts(w http.ResponseWriter, r *http.Request) {
	userID, err := pathID(r, "user_id")
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid user_id")
		return
	}
	viewerID, _ := h.currentUserID(r)
	page, perPage, offset := h.pagination(r)

	// type=replies なら返信のみ、それ以外は返信を除いた投稿のみを返す
	parentCond := "parent_post_id IS NULL"
	if r.URL.Query().Get("type") == "replies" {
		parentCond = "parent_post_id IS NOT NULL"
	}

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT id FROM posts WHERE user_id = ? AND `+parentCond+`
		ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?
	`, userID, perPage, offset)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			h.respondError(w, http.StatusInternalServerError, "server error")
			return
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}

	fetched, err := h.fetchPostsByIDs(r, ids, viewerID)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	posts := make([]any, 0, len(ids))
	for _, id := range ids {
		if p, ok := fetched[id]; ok {
			posts = append(posts, p)
		}
	}

	var total int
	h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM posts WHERE user_id = ? AND `+parentCond, userID,
	).Scan(&total)

	h.respondJSON(w, http.StatusOK, map[string]any{
		"posts":    posts,
		"total":    total,
		"page":     page,
		"per_page": perPage,
	})
}

func (h *Handler) GetPost(w http.ResponseWriter, r *http.Request) {
	postID, err := pathID(r, "id")
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid id")
		return
	}

	viewerID, _ := h.currentUserID(r)
	post, err := h.fetchPost(r, postID, viewerID)
	if err == sql.ErrNoRows {
		h.respondError(w, http.StatusNotFound, "post not found")
		return
	}
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	h.respondJSON(w, http.StatusOK, map[string]any{"post": post})
}

func (h *Handler) DeletePost(w http.ResponseWriter, r *http.Request) {
	myID, _ := h.currentUserID(r)
	postID, err := pathID(r, "id")
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid id")
		return
	}

	res, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM posts WHERE id = ? AND user_id = ?`, postID, myID,
	)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		h.respondError(w, http.StatusNotFound, "post not found or not yours")
		return
	}
	h.respondJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}
