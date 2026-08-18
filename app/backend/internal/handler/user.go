package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	myID, ok := h.currentUserID(r)
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	user, err := h.fetchUser(r, myID)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	h.respondJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	userID, err := pathID(r, "user_id")
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid user_id")
		return
	}

	user, err := h.fetchUser(r, userID)
	if err == sql.ErrNoRows {
		h.respondError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}

	// 足跡を記録（認証済みユーザーのみ）
	if visitorID, ok := h.currentUserID(r); ok {
		recordFootprint(h, r, userID, visitorID)
		createNotification(h, r, userID, "footprint", visitorID, nil)
	}

	h.respondJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	myID, _ := h.currentUserID(r)

	var req struct {
		DisplayName string  `json:"display_name"`
		Bio         *string `json:"bio"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	_, err := h.DB.ExecContext(r.Context(),
		`UPDATE users SET display_name = ?, bio = ? WHERE id = ?`,
		req.DisplayName, req.Bio, myID,
	)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}

	user, _ := h.fetchUser(r, myID)
	h.respondJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (h *Handler) GetFollowers(w http.ResponseWriter, r *http.Request) {
	userID, err := pathID(r, "user_id")
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid user_id")
		return
	}

	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT follower_id FROM follows WHERE followee_id = ?`, userID,
	)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var followerID int64
		if err := rows.Scan(&followerID); err != nil {
			h.respondError(w, http.StatusInternalServerError, "server error")
			return
		}
		ids = append(ids, followerID)
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
	h.respondJSON(w, http.StatusOK, map[string]any{"users": users, "total": len(users)})
}

func (h *Handler) GetFollowing(w http.ResponseWriter, r *http.Request) {
	userID, err := pathID(r, "user_id")
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid user_id")
		return
	}

	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT followee_id FROM follows WHERE follower_id = ?`, userID,
	)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var followeeID int64
		if err := rows.Scan(&followeeID); err != nil {
			h.respondError(w, http.StatusInternalServerError, "server error")
			return
		}
		ids = append(ids, followeeID)
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
	h.respondJSON(w, http.StatusOK, map[string]any{"users": users, "total": len(users)})
}

func (h *Handler) Follow(w http.ResponseWriter, r *http.Request) {
	myID, _ := h.currentUserID(r)
	targetID, err := pathID(r, "user_id")
	if err != nil || myID == targetID {
		h.respondError(w, http.StatusBadRequest, "invalid user_id")
		return
	}

	_, err = h.DB.ExecContext(r.Context(),
		`INSERT INTO follows (follower_id, followee_id) VALUES (?, ?)
		 ON DUPLICATE KEY UPDATE follower_id = follower_id`,
		myID, targetID,
	)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}

	createNotification(h, r, targetID, "follow", myID, nil)

	h.respondJSON(w, http.StatusOK, map[string]string{"message": "followed"})
}

func (h *Handler) Unfollow(w http.ResponseWriter, r *http.Request) {
	myID, _ := h.currentUserID(r)
	targetID, err := pathID(r, "user_id")
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid user_id")
		return
	}

	h.DB.ExecContext(r.Context(),
		`DELETE FROM follows WHERE follower_id = ? AND followee_id = ?`,
		myID, targetID,
	)
	h.respondJSON(w, http.StatusOK, map[string]string{"message": "unfollowed"})
}
