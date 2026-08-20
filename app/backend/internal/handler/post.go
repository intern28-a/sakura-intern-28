package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sakuravel/internal/ranking"
)

// scanIDs は ID 1列だけを返すクエリの結果を読み取る。
func scanIDs(rows *sql.Rows) ([]int64, error) {
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// recommendedLive はランキングを使わず、その場で直近24時間のいいねを集約する。
// ランキングの範囲外の深いページと、ランキングが未生成のときに使う。
func (h *Handler) recommendedLive(r *http.Request, perPage, offset int) (*sql.Rows, error) {
	return h.DB.QueryContext(r.Context(), `
		SELECT p.id
		FROM posts p
		LEFT JOIN (
			SELECT post_id, COUNT(*) AS c
			FROM likes
			WHERE created_at > NOW() - INTERVAL 24 HOUR
			GROUP BY post_id
		) l ON l.post_id = p.id
		WHERE p.parent_post_id IS NULL
		ORDER BY COALESCE(l.c, 0) DESC, p.created_at DESC, p.id DESC
		LIMIT ? OFFSET ?
	`, perPage, offset)
}

func (h *Handler) GetTimeline(w http.ResponseWriter, r *http.Request) {
	myID, _ := h.currentUserID(r)
	page, perPage, offset := h.pagination(r)
	feed := r.URL.Query().Get("feed")

	var rows *sql.Rows
	var totalRow *sql.Row
	var err error

	// 返信（parent_post_id あり）はスレッド画面でのみ表示するためタイムラインから除く
	switch feed {
	case "latest":
		rows, err = h.DB.QueryContext(r.Context(), `
			SELECT id
			FROM posts
			WHERE parent_post_id IS NULL
			ORDER BY created_at DESC, id DESC
			LIMIT ? OFFSET ?
		`, perPage, offset)
		totalRow = h.DB.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM posts WHERE parent_post_id IS NULL`,
		)
	case "recommended":
		// いいねの集約は EVENT が定期的に post_ranking へ書き出している。
		// 上位 TopN 件はそこを順位で引くだけで済む。
		if offset+perPage <= ranking.TopN {
			rows, err = h.DB.QueryContext(r.Context(), `
				SELECT post_id FROM post_ranking
				WHERE window_key = ?
				ORDER BY rank_pos
				LIMIT ? OFFSET ?
			`, ranking.Window24h, perPage, offset)
		} else {
			rows, err = h.recommendedLive(r, perPage, offset)
		}
		totalRow = h.DB.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM posts WHERE parent_post_id IS NULL`,
		)
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
		totalRow = h.DB.QueryRowContext(r.Context(), `
			SELECT COUNT(*) FROM posts
			WHERE parent_post_id IS NULL
			  AND user_id IN (
				SELECT followee_id FROM follows WHERE follower_id = ?
			)
		`, myID)
	}
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	defer rows.Close()

	ids, err := scanIDs(rows)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}

	// EVENT がまだ一度も走っていないと post_ranking は空になる。
	// その場合はその場で集約して返す（DB 作成直後や EVENT 停止時の保険）。
	if feed == "recommended" && len(ids) == 0 && offset == 0 {
		liveRows, liveErr := h.recommendedLive(r, perPage, offset)
		if liveErr != nil {
			h.respondError(w, http.StatusInternalServerError, "server error")
			return
		}
		defer liveRows.Close()
		ids, err = scanIDs(liveRows)
		if err != nil {
			h.respondError(w, http.StatusInternalServerError, "server error")
			return
		}
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
	if totalRow != nil {
		totalRow.Scan(&total)
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
