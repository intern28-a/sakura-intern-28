package handler

import (
	"net/http"
	"sakuravel/internal/realtime"
)

func (h *Handler) GetNotifications(w http.ResponseWriter, r *http.Request) {
	myID, _ := h.currentUserID(r)
	page, perPage, offset := h.pagination(r)

	// type で種別を絞る（reply / like / repost / follow / footprint）。
	// 空または all のときは全種別を返す。
	typeCond := ""
	typeArgs := []any{}
	if t := r.URL.Query().Get("type"); t != "" && t != "all" {
		typeCond = " AND type = ?"
		typeArgs = append(typeArgs, t)
	}

	listArgs := append([]any{myID}, typeArgs...)
	listArgs = append(listArgs, perPage, offset)
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT id, type, actor_id, post_id, is_read, created_at
		FROM notifications
		WHERE user_id = ?`+typeCond+`
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?
	`, listArgs...)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	defer rows.Close()

	type notifRow struct {
		id      int64
		ntype   string
		actorID int64
		postID  *int64
		isRead  bool
	}
	var rawNotifs []notifRow
	for rows.Next() {
		var n notifRow
		var createdAt any
		if err := rows.Scan(&n.id, &n.ntype, &n.actorID, &n.postID, &n.isRead, &createdAt); err != nil {
			h.respondError(w, http.StatusInternalServerError, "server error")
			return
		}
		rawNotifs = append(rawNotifs, n)
	}
	if err := rows.Err(); err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}

	// アクターと、抜粋に使う投稿をそれぞれまとめて取得する。
	// 従来は通知1件ごとにアクター5クエリ + 抜粋2クエリを発行していた。
	actorIDs := make([]int64, 0, len(rawNotifs))
	targetIDs := make([]int64, 0, len(rawNotifs))
	for _, rn := range rawNotifs {
		actorIDs = append(actorIDs, rn.actorID)
		if rn.postID != nil {
			targetIDs = append(targetIDs, *rn.postID)
		}
	}

	actors, err := h.fetchUsersByIDs(r, actorIDs)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}

	// 通知対象の投稿本文と、その親（返信通知向け）の本文を2クエリで解決する
	excerpts, parents, err := h.postExcerpts(r, targetIDs)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	parentIDs := make([]int64, 0, len(parents))
	for _, pid := range parents {
		parentIDs = append(parentIDs, pid)
	}
	parentExcerpts, _, err := h.postExcerpts(r, parentIDs)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}

	notifs := make([]any, 0, len(rawNotifs))
	for _, rn := range rawNotifs {
		actor, ok := actors[rn.actorID]
		if !ok {
			continue
		}

		// 対象投稿の本文の抜粋を付ける。
		// 返信通知の post_id は返信そのものを指すので、返信先の抜粋も添える。
		var excerpt, parentExcerpt *string
		if rn.postID != nil {
			excerpt = excerpts[*rn.postID]
			if parentID, ok := parents[*rn.postID]; ok {
				parentExcerpt = parentExcerpts[parentID]
			}
		}

		notifs = append(notifs, map[string]any{
			"id":             rn.id,
			"type":           rn.ntype,
			"actor":          actor,
			"post_id":        rn.postID,
			"post_excerpt":   excerpt,
			"parent_excerpt": parentExcerpt,
			"is_read":        rn.isRead,
		})
	}

	var unreadCount int
	h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM notifications WHERE user_id = ? AND is_read = FALSE`,
		myID,
	).Scan(&unreadCount)

	// total は絞り込み後の件数（ページングに使う）。unread_count はバッジ用なので全種別のまま。
	var total int
	h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM notifications WHERE user_id = ?`+typeCond,
		append([]any{myID}, typeArgs...)...,
	).Scan(&total)

	h.respondJSON(w, http.StatusOK, map[string]any{
		"notifications": notifs,
		"total":         total,
		"unread_count":  unreadCount,
		"page":          page,
		"per_page":      perPage,
	})
}

func (h *Handler) MarkNotificationsRead(w http.ResponseWriter, r *http.Request) {
	myID, _ := h.currentUserID(r)
	h.DB.ExecContext(r.Context(),
		`UPDATE notifications SET is_read = TRUE WHERE user_id = ? AND is_read = FALSE`,
		myID,
	)
	h.respondJSON(w, http.StatusOK, map[string]string{"message": "ok"})
}

func (h *Handler) GetUnreadCount(w http.ResponseWriter, r *http.Request) {
	myID, _ := h.currentUserID(r)
	var count int
	h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM notifications WHERE user_id = ? AND is_read = FALSE`,
		myID,
	).Scan(&count)
	h.respondJSON(w, http.StatusOK, map[string]int{"unread_count": count})
}

// postExcerpts は複数投稿の本文の抜粋と、返信元の投稿IDを1クエリでまとめて返す。
func (h *Handler) postExcerpts(r *http.Request, ids []int64) (map[int64]*string, map[int64]int64, error) {
	ids = uniqueIDs(ids)
	excerpts := make(map[int64]*string, len(ids))
	parents := make(map[int64]int64, len(ids))
	if len(ids) == 0 {
		return excerpts, parents, nil
	}

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT id, content, parent_post_id FROM posts
		WHERE id IN (`+placeholders(len(ids))+`)
	`, idArgs(ids)...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var content *string
		var parentID *int64
		if err := rows.Scan(&id, &content, &parentID); err != nil {
			return nil, nil, err
		}
		excerpts[id] = excerptOf(content)
		if parentID != nil {
			parents[id] = *parentID
		}
	}
	return excerpts, parents, rows.Err()
}

// excerptOf は通知一覧に載せる本文の抜粋を返す。
func excerptOf(content *string) *string {
	if content == nil {
		return nil
	}
	const limit = 40
	runes := []rune(*content)
	if len(runes) <= limit {
		return content
	}
	s := string(runes[:limit]) + "…"
	return &s
}

func createNotification(h *Handler, r *http.Request, userID int64, ntype string, actorID int64, postID *int64) {
	if userID == actorID {
		return
	}
	_, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO notifications (user_id, type, actor_id, post_id) VALUES (?, ?, ?, ?)`,
		userID, ntype, actorID, postID,
	)
	if err != nil {
		return
	}

	// 宛先ユーザーが SSE で接続していればバッジ更新用に通知する
	h.Notifications.Publish(userID, realtime.Event{
		Type: "notification",
		Data: map[string]any{"type": ntype, "post_id": postID},
	})
}
