package handler

import (
	"net/http"
)

func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	searchType := r.URL.Query().Get("type")
	if searchType == "" {
		searchType = "posts"
	}
	page, perPage, offset := h.pagination(r)
	viewerID, _ := h.currentUserID(r)

	if q == "" {
		h.respondError(w, http.StatusBadRequest, "q is required")
		return
	}

	switch searchType {
	case "posts":
		h.searchPosts(w, r, q, viewerID, page, perPage, offset)
	case "users":
		h.searchUsers(w, r, q, page, perPage, offset)
	default:
		h.respondError(w, http.StatusBadRequest, "type must be posts or users")
	}
}

func (h *Handler) searchPosts(w http.ResponseWriter, r *http.Request, q string, viewerID int64, page, perPage, offset int) {
	pattern := "%" + q + "%"
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT id FROM posts
		WHERE content LIKE ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, pattern, perPage, offset)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	defer rows.Close()

	postIDs := make([]int64, 0)
	for rows.Next() {
		var postID int64
		rows.Scan(&postID)
		postIDs = append(postIDs, postID)
	}

	posts := make([]any, 0, len(postIDs))
	for _, postID := range postIDs {
		p, err := h.fetchPost(r, postID, viewerID)
		if err == nil {
			posts = append(posts, p)
		}
	}

	var total int
	h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM posts WHERE content LIKE ?`, pattern,
	).Scan(&total)

	h.respondJSON(w, http.StatusOK, map[string]any{
		"posts":    posts,
		"total":    total,
		"page":     page,
		"per_page": perPage,
	})
}

func (h *Handler) searchUsers(w http.ResponseWriter, r *http.Request, q string, page, perPage, offset int) {
	pattern := "%" + q + "%"
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT id FROM users
		WHERE username LIKE ? OR display_name LIKE ?
		ORDER BY id
		LIMIT ? OFFSET ?
	`, pattern, pattern, perPage, offset)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	defer rows.Close()

	userIDs := make([]int64, 0)
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			h.respondError(w, http.StatusInternalServerError, "server error")
			return
		}
		userIDs = append(userIDs, uid)
	}
	if err := rows.Err(); err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}

	fetched, err := h.fetchUsersByIDs(r, userIDs)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	users := make([]any, 0, len(userIDs))
	for _, uid := range userIDs {
		if u, ok := fetched[uid]; ok {
			users = append(users, u)
		}
	}

	var total int
	h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM users WHERE username LIKE ? OR display_name LIKE ?`, pattern, pattern,
	).Scan(&total)

	h.respondJSON(w, http.StatusOK, map[string]any{
		"users":    users,
		"total":    total,
		"page":     page,
		"per_page": perPage,
	})
}
