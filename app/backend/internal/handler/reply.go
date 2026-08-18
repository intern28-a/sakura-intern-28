package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sakuravel/internal/model"
	"sakuravel/internal/realtime"
)

// CreateReply は指定した投稿への返信を作成する。返信も posts の1行として保存する。
func (h *Handler) CreateReply(w http.ResponseWriter, r *http.Request) {
	myID, _ := h.currentUserID(r)

	var req struct {
		PostID  int64  `json:"post_id"`
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

	parentID := req.PostID
	var parentAuthorID int64
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT user_id FROM posts WHERE id = ?`, parentID,
	).Scan(&parentAuthorID)
	if err == sql.ErrNoRows {
		h.respondError(w, http.StatusNotFound, "post not found")
		return
	}
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}

	res, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO posts (user_id, content, parent_post_id) VALUES (?, ?, ?)`,
		myID, req.Content, parentID,
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

	// 通知は直接の返信先の著者にのみ送る
	createNotification(h, r, parentAuthorID, "reply", myID, &postID)

	// 同じスレッドを開いている購読者へリアルタイム配信する
	h.Threads.Publish(h.threadRootID(r, parentID), realtime.Event{Type: "reply", Data: post})

	h.respondJSON(w, http.StatusCreated, map[string]any{"post": post})
}

// GetThread は対象投稿と、その祖先チェーン・返信ツリーをまとめて返す。
func (h *Handler) GetThread(w http.ResponseWriter, r *http.Request) {
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

	// 祖先の ID を先に集めてから、本体をまとめて取得する
	ancestorIDs := h.ancestorIDs(r, post.ParentPostID)
	fetchedAncestors, err := h.fetchPostsByIDs(r, ancestorIDs, viewerID)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	// 祖先は古い順に並べ替えて返す
	ancestors := make([]any, 0, len(ancestorIDs))
	for i := len(ancestorIDs) - 1; i >= 0; i-- {
		if a, ok := fetchedAncestors[ancestorIDs[i]]; ok {
			ancestors = append(ancestors, a)
		}
	}

	h.respondJSON(w, http.StatusOK, map[string]any{
		"ancestors": ancestors,
		"post":      post,
		"replies":   h.fetchReplyTree(r, postID, viewerID),
	})
}

// ancestorIDs は返信先をさかのぼった投稿ID列を近い順に返す。
func (h *Handler) ancestorIDs(r *http.Request, parentID *int64) []int64 {
	ids := make([]int64, 0)
	current := parentID
	for depth := 0; current != nil && depth < maxThreadDepth; depth++ {
		ids = append(ids, *current)
		var next *int64
		if err := h.DB.QueryRowContext(r.Context(),
			`SELECT parent_post_id FROM posts WHERE id = ?`, *current,
		).Scan(&next); err != nil {
			break
		}
		current = next
	}
	return ids
}

// fetchReplyTree は子返信をツリー状に取得する。
// ツリーの構造を1階層につき1クエリで集めてから、投稿本体をまとめて取得する。
// 従来はノード1つごとに構造クエリ + 投稿取得（11クエリ以上）を発行していた。
func (h *Handler) fetchReplyTree(r *http.Request, postID, viewerID int64) []any {
	children := make(map[int64][]int64)
	all := make([]int64, 0)

	level := []int64{postID}
	for depth := 0; depth < maxThreadDepth && len(level) > 0; depth++ {
		byParent, err := h.childIDsByParent(r, level)
		if err != nil {
			break
		}
		next := make([]int64, 0)
		for parentID, ids := range byParent {
			children[parentID] = ids
			next = append(next, ids...)
		}
		all = append(all, next...)
		level = next
	}

	posts, err := h.fetchPostsByIDs(r, all, viewerID)
	if err != nil {
		return make([]any, 0)
	}
	return buildReplyTree(postID, children, posts, 0)
}

// buildReplyTree は取得済みの投稿からツリーを組み立てる（DBアクセスなし）。
func buildReplyTree(parentID int64, children map[int64][]int64, posts map[int64]model.Post, depth int) []any {
	nodes := make([]any, 0, len(children[parentID]))
	if depth >= maxThreadDepth {
		return nodes
	}
	for _, id := range children[parentID] {
		p, ok := posts[id]
		if !ok {
			continue
		}
		nodes = append(nodes, map[string]any{
			"post":    p,
			"replies": buildReplyTree(id, children, posts, depth+1),
		})
	}
	return nodes
}
