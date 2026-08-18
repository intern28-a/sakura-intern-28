package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sakuravel/internal/middleware"
	"sakuravel/internal/model"
	"sakuravel/internal/realtime"
	"strconv"
)

type Handler struct {
	DB *sql.DB
	// CookieSecure が true の場合、セッションCookieに Secure + SameSite=None を付与する。
	// フロントエンドとバックエンドが別オリジン（別サブドメイン等）で動く構成向け。
	CookieSecure bool
	// Notifications はユーザーIDごと、Threads はスレッドのルート投稿IDごとの SSE 購読を管理する。
	Notifications *realtime.Hub
	Threads       *realtime.Hub
}

func (h *Handler) respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) respondError(w http.ResponseWriter, status int, msg string) {
	h.respondJSON(w, status, map[string]string{"error": msg})
}

func (h *Handler) currentUserID(r *http.Request) (int64, bool) {
	id, ok := r.Context().Value(middleware.UserIDKey).(int64)
	return id, ok
}

func (h *Handler) pagination(r *http.Request) (page, perPage, offset int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ = strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 || perPage > 50 {
		perPage = 20
	}
	offset = (page - 1) * perPage
	return
}

// fetchUser は users テーブルから1件取得する
func (h *Handler) fetchUser(r *http.Request, userID int64) (model.User, error) {
	var u model.User
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT id, username, display_name, bio, created_at FROM users WHERE id = ?`,
		userID,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Bio, &u.CreatedAt)
	if err != nil {
		return u, err
	}
	u.AvatarColor = model.AvatarColor(u.ID)

	// フォロワー数・フォロー数・投稿数を取得
	h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM follows WHERE followee_id = ?`, u.ID,
	).Scan(&u.FollowersCount)

	h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM follows WHERE follower_id = ?`, u.ID,
	).Scan(&u.FollowingCount)

	h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM posts WHERE user_id = ?`, u.ID,
	).Scan(&u.PostCount)

	if viewerID, ok := h.currentUserID(r); ok && viewerID != u.ID {
		h.DB.QueryRowContext(r.Context(),
			`SELECT EXISTS(SELECT 1 FROM follows WHERE follower_id = ? AND followee_id = ?)`,
			viewerID, u.ID,
		).Scan(&u.FollowedByMe)
	}

	return u, nil
}

// fetchPost は posts テーブルから1件取得し、関連データを付加する
func (h *Handler) fetchPost(r *http.Request, postID, viewerID int64) (model.Post, error) {
	var p model.Post
	var userID int64
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT id, user_id, content, is_repost, original_post_id, parent_post_id, created_at
		 FROM posts WHERE id = ?`,
		postID,
	).Scan(&p.ID, &userID, &p.Content, &p.IsRepost, &p.OriginalPostID, &p.ParentPostID, &p.CreatedAt)
	if err != nil {
		return p, err
	}

	author, err := h.fetchUser(r, userID)
	if err != nil {
		return p, err
	}
	p.Author = author

	h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM likes WHERE post_id = ?`, p.ID,
	).Scan(&p.LikesCount)

	h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM reposts WHERE post_id = ?`, p.ID,
	).Scan(&p.RepostsCount)

	p.RepliesCount = h.countReplies(r, p.ID, 0)

	if viewerID > 0 {
		h.DB.QueryRowContext(r.Context(),
			`SELECT EXISTS(SELECT 1 FROM likes WHERE user_id = ? AND post_id = ?)`,
			viewerID, p.ID,
		).Scan(&p.LikedByMe)

		h.DB.QueryRowContext(r.Context(),
			`SELECT EXISTS(SELECT 1 FROM reposts WHERE user_id = ? AND post_id = ?)`,
			viewerID, p.ID,
		).Scan(&p.RepostedByMe)
	}

	// 返信の場合、返信先の投稿者を解決する
	if p.ParentPostID != nil {
		var username, displayName string
		err := h.DB.QueryRowContext(r.Context(), `
			SELECT u.username, u.display_name
			FROM posts parent JOIN users u ON u.id = parent.user_id
			WHERE parent.id = ?
		`, *p.ParentPostID).Scan(&username, &displayName)
		if err == nil {
			p.ReplyToUsername = &username
			p.ReplyToDisplayName = &displayName
		}
	}

	// リポストの場合、何をリポストしたか分かるように元投稿を解決する
	if p.IsRepost && p.OriginalPostID != nil && *p.OriginalPostID != p.ID {
		if original, err := h.fetchPost(r, *p.OriginalPostID, viewerID); err == nil {
			p.OriginalPost = &original
		}
	}

	return p, nil
}

// maxThreadDepth はスレッドを辿る深さの上限（循環や極端に深いスレッドの保険）。
const maxThreadDepth = 50

// countReplies は投稿にぶら下がる返信の数を返す。ネストした返信も含めた合計。
func (h *Handler) countReplies(r *http.Request, postID int64, depth int) int {
	var total int

	if depth >= maxThreadDepth {
		return 0
	}
	err := h.DB.QueryRowContext(r.Context(), `
		WITH RECURSIVE d AS (
    		SELECT id, 1 AS depth FROM posts WHERE parent_post_id = ?
    		UNION ALL
    		SELECT p.id, d.depth + 1
    		FROM posts p JOIN d ON p.parent_post_id = d.id
    		WHERE d.depth < ?
		)
		SELECT COUNT(*) FROM d;
	`, postID, maxThreadDepth).Scan(&total)
	if err != nil {
		return 0
	}

	return total
}

// threadRootID はスレッドの起点となる投稿IDを返す。
func (h *Handler) threadRootID(r *http.Request, postID int64) int64 {
	current := postID
	for i := 0; i < maxThreadDepth; i++ {
		var parent *int64
		err := h.DB.QueryRowContext(r.Context(),
			`SELECT parent_post_id FROM posts WHERE id = ?`, current,
		).Scan(&parent)
		if err != nil || parent == nil {
			return current
		}
		current = *parent
	}
	return current
}

func pathID(r *http.Request, key string) (int64, error) {
	return strconv.ParseInt(r.PathValue(key), 10, 64)
}
