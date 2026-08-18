package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	appdb "sakuravel/internal/db"
	"sakuravel/internal/handler"
	"sakuravel/internal/middleware"
	"sakuravel/internal/realtime"
)

func main() {
	db := appdb.New()
	defer db.Close()

	h := &handler.Handler{
		DB:             db,
		CookieSecure:   os.Getenv("COOKIE_SECURE") == "true",
		CookieSameSite: cookieSameSite(),
		Notifications:  realtime.NewHub(),
		Threads:        realtime.NewHub(),
	}
	auth := &middleware.Auth{DB: db}

	mux := http.NewServeMux()

	// CORS ミドルウェア
	mux.Handle("/", corsMiddleware(routes(h, auth)))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// SSE でレスポンスを流し続けるため WriteTimeout は設定しない。
	// ヘッダ読み取りだけ打ち切って、リバースプロキシ手前での接続滞留を防ぐ。
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("starting server on :%s", port)
	log.Fatal(srv.ListenAndServe())
}

// cookieSameSite は COOKIE_SAMESITE からセッションCookieの SameSite 属性を決める。
// 同一オリジン構成では既定の Lax で足りる。None はフロントと別オリジンになる構成用。
func cookieSameSite() http.SameSite {
	switch strings.ToLower(os.Getenv("COOKIE_SAMESITE")) {
	case "none":
		return http.SameSiteNoneMode
	case "strict":
		return http.SameSiteStrictMode
	default:
		return http.SameSiteLaxMode
	}
}

func routes(h *handler.Handler, auth *middleware.Auth) http.Handler {
	mux := http.NewServeMux()

	// ヘルスチェック（リバースプロキシ／LB からの死活監視用）
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok"))
	})

	// 認証
	mux.HandleFunc("POST /register", h.Register)
	mux.HandleFunc("POST /login", h.Login)
	mux.Handle("POST /logout", auth.Required(http.HandlerFunc(h.Logout)))

	// 自分のプロフィール
	mux.Handle("GET /me", auth.Required(http.HandlerFunc(h.GetMe)))

	// 足跡
	mux.Handle("GET /me/footprints", auth.Required(http.HandlerFunc(h.GetFootprints)))

	// ユーザー
	mux.Handle("GET /profile/{user_id}", auth.Optional(http.HandlerFunc(h.GetProfile)))
	mux.Handle("PUT /profile", auth.Required(http.HandlerFunc(h.UpdateProfile)))
	mux.Handle("GET /users/{user_id}/followers", auth.Optional(http.HandlerFunc(h.GetFollowers)))
	mux.Handle("GET /users/{user_id}/following", auth.Optional(http.HandlerFunc(h.GetFollowing)))
	mux.Handle("POST /users/{user_id}/follow", auth.Required(http.HandlerFunc(h.Follow)))
	mux.Handle("DELETE /users/{user_id}/follow", auth.Required(http.HandlerFunc(h.Unfollow)))

	// 投稿
	mux.Handle("GET /posts", auth.Required(http.HandlerFunc(h.GetTimeline)))
	mux.Handle("POST /posts", auth.Required(http.HandlerFunc(h.CreatePost)))
	mux.Handle("GET /posts/{id}", auth.Optional(http.HandlerFunc(h.GetPost)))
	mux.Handle("GET /users/{user_id}/posts", auth.Optional(http.HandlerFunc(h.GetUserPosts)))
	mux.Handle("DELETE /posts/{id}", auth.Required(http.HandlerFunc(h.DeletePost)))

	// 返信（スレッド）
	mux.Handle("POST /replies", auth.Required(http.HandlerFunc(h.CreateReply)))
	mux.Handle("GET /posts/{id}/thread", auth.Optional(http.HandlerFunc(h.GetThread)))
	mux.Handle("GET /posts/{id}/thread/stream", auth.Optional(http.HandlerFunc(h.ThreadStream)))

	// いいね
	mux.HandleFunc("GET /posts/{id}/likes", h.GetLikes)
	mux.Handle("POST /likes", auth.Required(http.HandlerFunc(h.Like)))
	mux.Handle("DELETE /likes/{post_id}", auth.Required(http.HandlerFunc(h.Unlike)))

	// リポスト
	mux.Handle("POST /reposts", auth.Required(http.HandlerFunc(h.Repost)))
	mux.Handle("DELETE /reposts/{post_id}", auth.Required(http.HandlerFunc(h.UnRepost)))

	// 検索
	mux.HandleFunc("GET /search", h.Search)

	// 通知
	mux.Handle("GET /notifications", auth.Required(http.HandlerFunc(h.GetNotifications)))
	mux.Handle("POST /notifications/read", auth.Required(http.HandlerFunc(h.MarkNotificationsRead)))
	mux.Handle("GET /notifications/unread_count", auth.Required(http.HandlerFunc(h.GetUnreadCount)))
	mux.Handle("GET /notifications/stream", auth.Required(http.HandlerFunc(h.NotificationStream)))

	// トレンド
	mux.Handle("GET /trending", auth.Optional(http.HandlerFunc(h.GetTrending)))

	return mux
}

func corsMiddleware(next http.Handler) http.Handler {
	allowedOrigin := os.Getenv("ALLOWED_ORIGIN")
	// リバースプロキシ配下でフロントと同一オリジンに載せる構成では CORS 自体が不要。
	// ALLOWED_ORIGIN が未設定ならヘッダを付けず、プリフライトも発生させない。
	if allowedOrigin == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Add("Vary", "Origin")
		if r.Method == http.MethodOptions {
			// プリフライトの結果をキャッシュさせ、1リクエストごとの往復を減らす。
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
