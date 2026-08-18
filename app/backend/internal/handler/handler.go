package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sakuravel/internal/middleware"
	"sakuravel/internal/model"
	"sakuravel/internal/realtime"
	"strconv"
	"strings"
)

type Handler struct {
	DB *sql.DB
	// CookieSecure が true の場合、セッションCookieに Secure を付与する。HTTPS 配信時は必須。
	CookieSecure bool
	// CookieSameSite はセッションCookieの SameSite 属性。ゼロ値のときは Lax として扱う。
	// フロントエンドとバックエンドが別オリジンになる構成でのみ None にする（Secure が前提）。
	CookieSameSite http.SameSite
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

// maxThreadDepth はスレッドを辿る深さの上限（循環や極端に深いスレッドの保険）。
const maxThreadDepth = 50

// maxRepostChainDepth はリポストの元投稿をたどる段数の上限。
// 1段につきクエリ1回なので、長い連鎖への保険を兼ねる。
const maxRepostChainDepth = 10

////////////////////////////////////////////////////////////////////////////
// 一括取得のための小道具
////////////////////////////////////////////////////////////////////////////

// placeholders は "?,?,?" のようなプレースホルダ列を組み立てる。
func placeholders(n int) string {
	if n == 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// idArgs は ID 列を QueryContext の可変長引数に変換する。
func idArgs(ids []int64) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}

// uniqueIDs は重複を除いた ID 列を返す（出現順は維持する）。
func uniqueIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

////////////////////////////////////////////////////////////////////////////
// ユーザー
////////////////////////////////////////////////////////////////////////////

// fetchUsersByIDs は複数ユーザーを1クエリでまとめて取得する。
// フォロワー数・フォロー数・投稿数・フォロー済みかどうかも同じクエリで解決するため、
// ユーザー1人あたり5クエリ発行していたものが全体で1クエリになる。
func (h *Handler) fetchUsersByIDs(r *http.Request, ids []int64) (map[int64]model.User, error) {
	ids = uniqueIDs(ids)
	out := make(map[int64]model.User, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	viewerID, _ := h.currentUserID(r)

	args := make([]any, 0, len(ids)+1)
	args = append(args, viewerID)
	args = append(args, idArgs(ids)...)

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT u.id, u.username, u.display_name, u.bio, u.created_at,
		       (SELECT COUNT(*) FROM follows f1 WHERE f1.followee_id = u.id) AS followers_count,
		       (SELECT COUNT(*) FROM follows f2 WHERE f2.follower_id = u.id) AS following_count,
		       (SELECT COUNT(*) FROM posts   p  WHERE p.user_id     = u.id) AS post_count,
		       EXISTS(SELECT 1 FROM follows f3
		              WHERE f3.follower_id = ? AND f3.followee_id = u.id) AS followed_by_me
		FROM users u
		WHERE u.id IN (`+placeholders(len(ids))+`)
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var u model.User
		if err := rows.Scan(
			&u.ID, &u.Username, &u.DisplayName, &u.Bio, &u.CreatedAt,
			&u.FollowersCount, &u.FollowingCount, &u.PostCount, &u.FollowedByMe,
		); err != nil {
			return nil, err
		}
		u.AvatarColor = model.AvatarColor(u.ID)
		// 自分自身に対しては followed_by_me を立てない（従来の挙動に合わせる）
		if u.ID == viewerID {
			u.FollowedByMe = false
		}
		out[u.ID] = u
	}
	return out, rows.Err()
}

// fetchUser は users テーブルから1件取得する。中身は一括取得の1件版。
func (h *Handler) fetchUser(r *http.Request, userID int64) (model.User, error) {
	users, err := h.fetchUsersByIDs(r, []int64{userID})
	if err != nil {
		return model.User{}, err
	}
	u, ok := users[userID]
	if !ok {
		return model.User{}, sql.ErrNoRows
	}
	return u, nil
}

////////////////////////////////////////////////////////////////////////////
// 投稿
////////////////////////////////////////////////////////////////////////////

// postRecord は投稿本体と、まだ解決していない著者IDを保持する中間表現。
type postRecord struct {
	post     model.Post
	authorID int64
	depth    int
}

// fetchPostsByIDs は複数投稿を一括で取得する。
//
// 発行するクエリは
//   - 投稿本体（いいね数・リポスト数・自分の反応・返信先の著者を含む）: リポスト連鎖の段数ぶん（通常1回）
//   - 著者ユーザー: 1回
//   - 返信数: 1回
//
// で、投稿件数に比例しない。従来は投稿1件あたり11クエリ以上発行していた。
func (h *Handler) fetchPostsByIDs(r *http.Request, ids []int64, viewerID int64) (map[int64]model.Post, error) {
	out := make(map[int64]model.Post)
	if len(ids) == 0 {
		return out, nil
	}

	records := make(map[int64]*postRecord)
	pending := uniqueIDs(ids)
	maxDepth := 0

	// リポストは元投稿をたどる必要がある。1段ぶんをまとめて引き、
	// 新たに判明した元投稿IDを次の段へ回す。
	for depth := 0; depth < maxRepostChainDepth && len(pending) > 0; depth++ {
		batch, err := h.loadPostRows(r, pending, viewerID)
		if err != nil {
			return nil, err
		}

		next := make([]int64, 0)
		for id, rec := range batch {
			rec.depth = depth
			records[id] = rec
			if depth > maxDepth {
				maxDepth = depth
			}

			orig := rec.post.OriginalPostID
			if rec.post.IsRepost && orig != nil && *orig != id {
				if _, done := records[*orig]; !done {
					next = append(next, *orig)
				}
			}
		}

		pending = pending[:0]
		for _, id := range uniqueIDs(next) {
			if _, done := records[id]; !done {
				pending = append(pending, id)
			}
		}
	}

	// 著者と返信数をそれぞれ1クエリでまとめて解決する
	authorIDs := make([]int64, 0, len(records))
	postIDs := make([]int64, 0, len(records))
	for id, rec := range records {
		authorIDs = append(authorIDs, rec.authorID)
		postIDs = append(postIDs, id)
	}

	users, err := h.fetchUsersByIDs(r, authorIDs)
	if err != nil {
		return nil, err
	}
	replyCounts, err := h.countRepliesByIDs(r, postIDs)
	if err != nil {
		return nil, err
	}

	// 浅い段は深い段を OriginalPost として参照するため、深い順に組み立てる
	for depth := maxDepth; depth >= 0; depth-- {
		for id, rec := range records {
			if rec.depth != depth {
				continue
			}
			author, ok := users[rec.authorID]
			if !ok {
				// 著者が存在しない投稿は従来どおり取得失敗として扱う
				continue
			}

			p := rec.post
			p.Author = author
			p.RepliesCount = replyCounts[id]

			if p.IsRepost && p.OriginalPostID != nil && *p.OriginalPostID != id {
				if original, ok := out[*p.OriginalPostID]; ok {
					copied := original
					p.OriginalPost = &copied
				}
			}
			out[id] = p
		}
	}

	return out, nil
}

// loadPostRows は投稿本体を1クエリで取得する。
// いいね数・リポスト数・自分の反応・返信先の著者を相関サブクエリと JOIN で同時に解決する。
func (h *Handler) loadPostRows(r *http.Request, ids []int64, viewerID int64) (map[int64]*postRecord, error) {
	args := make([]any, 0, len(ids)+2)
	args = append(args, viewerID, viewerID)
	args = append(args, idArgs(ids)...)

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT p.id, p.user_id, p.content, p.is_repost, p.original_post_id,
		       p.parent_post_id, p.created_at,
		       (SELECT COUNT(*) FROM likes   l  WHERE l.post_id  = p.id) AS likes_count,
		       (SELECT COUNT(*) FROM reposts rp WHERE rp.post_id = p.id) AS reposts_count,
		       EXISTS(SELECT 1 FROM likes   l2 WHERE l2.post_id = p.id AND l2.user_id = ?) AS liked_by_me,
		       EXISTS(SELECT 1 FROM reposts r2 WHERE r2.post_id = p.id AND r2.user_id = ?) AS reposted_by_me,
		       pu.username, pu.display_name
		FROM posts p
		LEFT JOIN posts parent ON parent.id = p.parent_post_id
		LEFT JOIN users pu     ON pu.id     = parent.user_id
		WHERE p.id IN (`+placeholders(len(ids))+`)
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int64]*postRecord, len(ids))
	for rows.Next() {
		var rec postRecord
		if err := rows.Scan(
			&rec.post.ID, &rec.authorID, &rec.post.Content, &rec.post.IsRepost,
			&rec.post.OriginalPostID, &rec.post.ParentPostID, &rec.post.CreatedAt,
			&rec.post.LikesCount, &rec.post.RepostsCount,
			&rec.post.LikedByMe, &rec.post.RepostedByMe,
			&rec.post.ReplyToUsername, &rec.post.ReplyToDisplayName,
		); err != nil {
			return nil, err
		}
		out[rec.post.ID] = &rec
	}
	return out, rows.Err()
}

// fetchPost は posts テーブルから1件取得し、関連データを付加する。中身は一括取得の1件版。
func (h *Handler) fetchPost(r *http.Request, postID, viewerID int64) (model.Post, error) {
	posts, err := h.fetchPostsByIDs(r, []int64{postID}, viewerID)
	if err != nil {
		return model.Post{}, err
	}
	p, ok := posts[postID]
	if !ok {
		return model.Post{}, sql.ErrNoRows
	}
	return p, nil
}

////////////////////////////////////////////////////////////////////////////
// 返信ツリー
////////////////////////////////////////////////////////////////////////////

// countRepliesByIDs は各投稿にぶら下がる返信の総数（ネストを含む）を1クエリで返す。
// 再帰CTEの起点を複数の投稿にすることで、投稿ごとにクエリを打つ必要がなくなる。
func (h *Handler) countRepliesByIDs(r *http.Request, ids []int64) (map[int64]int, error) {
	ids = uniqueIDs(ids)
	out := make(map[int64]int, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	args := append(idArgs(ids), maxThreadDepth)
	rows, err := h.DB.QueryContext(r.Context(), `
		WITH RECURSIVE d AS (
			SELECT id, parent_post_id AS root_id, 1 AS depth
			FROM posts WHERE parent_post_id IN (`+placeholders(len(ids))+`)
			UNION ALL
			SELECT p.id, d.root_id, d.depth + 1
			FROM posts p JOIN d ON p.parent_post_id = d.id
			WHERE d.depth < ?
		)
		SELECT root_id, COUNT(*) FROM d GROUP BY root_id
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var rootID int64
		var count int
		if err := rows.Scan(&rootID, &count); err != nil {
			return nil, err
		}
		out[rootID] = count
	}
	return out, rows.Err()
}

// childIDsByParent は指定した複数の親IDについて、その直接の子を1クエリでまとめて引く。
// 返信ツリーを1段ずつ幅優先で降りるために使う。
func (h *Handler) childIDsByParent(r *http.Request, parentIDs []int64) (map[int64][]int64, error) {
	parentIDs = uniqueIDs(parentIDs)
	out := make(map[int64][]int64, len(parentIDs))
	if len(parentIDs) == 0 {
		return out, nil
	}

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT id, parent_post_id FROM posts
		WHERE parent_post_id IN (`+placeholders(len(parentIDs))+`)
		ORDER BY created_at ASC, id ASC
	`, idArgs(parentIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, parentID int64
		if err := rows.Scan(&id, &parentID); err != nil {
			return nil, err
		}
		out[parentID] = append(out[parentID], id)
	}
	return out, rows.Err()
}

// ancestorChain は startID から親をさかのぼった投稿ID列を、近い順（startID を先頭に含む）で返す。
// 1件ずつ親を引くと最大 maxThreadDepth 回の逐次ラウンドトリップになるため、
// 上向きの再帰CTEで1クエリにまとめている。
func (h *Handler) ancestorChain(r *http.Request, startID int64) ([]int64, error) {
	rows, err := h.DB.QueryContext(r.Context(), `
		WITH RECURSIVE up AS (
			SELECT id, parent_post_id, 1 AS depth FROM posts WHERE id = ?
			UNION ALL
			SELECT p.id, p.parent_post_id, up.depth + 1
			FROM posts p JOIN up ON p.id = up.parent_post_id
			WHERE up.depth < ?
		)
		SELECT id FROM up ORDER BY depth
	`, startID, maxThreadDepth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]int64, 0, maxThreadDepth)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// threadRootID はスレッドの起点となる投稿IDを返す。
func (h *Handler) threadRootID(r *http.Request, postID int64) int64 {
	chain, err := h.ancestorChain(r, postID)
	if err != nil || len(chain) == 0 {
		return postID
	}
	return chain[len(chain)-1]
}

func pathID(r *http.Request, key string) (int64, error) {
	return strconv.ParseInt(r.PathValue(key), 10, 64)
}
