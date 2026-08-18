package handler

import (
	"net/http"
)

func recordFootprint(h *Handler, r *http.Request, userID, visitorID int64) {
	if userID == visitorID {
		return
	}
	h.DB.ExecContext(r.Context(),
		`INSERT INTO footprints (user_id, visitor_id) VALUES (?, ?)`,
		userID, visitorID,
	)
}

func (h *Handler) GetFootprints(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUserID(r)
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	page, perPage, offset := h.pagination(r)

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT visitor_id, COUNT(*) AS visit_count, MAX(created_at) AS last_visited
		FROM footprints
		WHERE user_id = ?
		GROUP BY visitor_id
		ORDER BY last_visited DESC, visitor_id DESC
		LIMIT ? OFFSET ?
	`, userID, perPage, offset)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	defer rows.Close()

	type fpRow struct {
		visitorID  int64
		visitCount int
	}
	var rawFps []fpRow
	for rows.Next() {
		var fp fpRow
		var lastVisited any
		if err := rows.Scan(&fp.visitorID, &fp.visitCount, &lastVisited); err != nil {
			h.respondError(w, http.StatusInternalServerError, "server error")
			return
		}
		rawFps = append(rawFps, fp)
	}
	if err := rows.Err(); err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}

	visitorIDs := make([]int64, 0, len(rawFps))
	for _, rf := range rawFps {
		visitorIDs = append(visitorIDs, rf.visitorID)
	}
	visitors, err := h.fetchUsersByIDs(r, visitorIDs)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}

	fps := make([]any, 0, len(rawFps))
	for _, rf := range rawFps {
		visitor, ok := visitors[rf.visitorID]
		if !ok {
			continue
		}
		fps = append(fps, map[string]any{
			"visitor":     visitor,
			"visit_count": rf.visitCount,
		})
	}

	var total int
	h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(DISTINCT visitor_id) FROM footprints WHERE user_id = ?`, userID,
	).Scan(&total)

	h.respondJSON(w, http.StatusOK, map[string]any{
		"footprints": fps,
		"total":      total,
		"page":       page,
		"per_page":   perPage,
	})
}
