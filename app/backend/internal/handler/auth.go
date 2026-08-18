package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (h *Handler) sessionCookie(value string, expiresAt time.Time) *http.Cookie {
	sameSite := h.CookieSameSite
	if sameSite == 0 {
		sameSite = http.SameSiteLaxMode
	}
	c := &http.Cookie{
		Name:     "session_id",
		Value:    value,
		Expires:  expiresAt,
		HttpOnly: true,
		Path:     "/",
		Secure:   h.CookieSecure,
		SameSite: sameSite,
	}
	// SameSite=None は Secure が付いていないとブラウザに破棄される。
	if sameSite == http.SameSiteNoneMode {
		c.Secure = true
	}
	return c
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
		Password    string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Username == "" || req.Email == "" || req.Password == "" {
		h.respondError(w, http.StatusBadRequest, "username, email, password are required")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}

	res, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO users (username, display_name, email, password_hash)
		 VALUES (?, ?, ?, ?)`,
		req.Username, req.DisplayName, req.Email, string(hash),
	)
	if err != nil {
		h.respondError(w, http.StatusConflict, "username or email already exists")
		return
	}
	userID, err := res.LastInsertId()
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}

	user, err := h.fetchUser(r, userID)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}

	// 登録と同時にセッションを発行してログイン状態にする
	token, err := generateToken()
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	expiresAt := time.Now().Add(24 * time.Hour)
	_, err = h.DB.ExecContext(r.Context(),
		`INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)`,
		token, userID, expiresAt,
	)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}
	http.SetCookie(w, h.sessionCookie(token, expiresAt))

	h.respondJSON(w, http.StatusCreated, map[string]any{"user": user})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	var userID int64
	var passwordHash string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT id, password_hash FROM users WHERE email = ?`, req.Email,
	).Scan(&userID, &passwordHash)
	if err != nil {
		h.respondError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		h.respondError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	token, err := generateToken()
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}

	expiresAt := time.Now().Add(24 * time.Hour)
	_, err = h.DB.ExecContext(r.Context(),
		`INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)`,
		token, userID, expiresAt,
	)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "server error")
		return
	}

	http.SetCookie(w, h.sessionCookie(token, expiresAt))

	user, _ := h.fetchUser(r, userID)
	h.respondJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "not logged in")
		return
	}

	h.DB.ExecContext(r.Context(), `DELETE FROM sessions WHERE id = ?`, cookie.Value)

	http.SetCookie(w, h.sessionCookie("", time.Unix(0, 0)))
	h.respondJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}
