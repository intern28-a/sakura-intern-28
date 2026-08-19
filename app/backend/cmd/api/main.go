package main

import (
	"log"
	"net/http"
	"os"
	"time"

	appdb "sakuravel/internal/db"
	"sakuravel/internal/handler"
	"sakuravel/internal/middleware"
	"sakuravel/internal/realtime"

	"github.com/patrickmn/go-cache"
)

func main() {
	db := appdb.New()
	defer db.Close()

	h := &handler.Handler{
		DB:            db,
		TrendingCache: cache.New(30*time.Second, 1*time.Minute),
		CookieSecure:  os.Getenv("COOKIE_SECURE") == "true",
		Notifications: realtime.NewHub(),
		Threads:       realtime.NewHub(),
	}
	auth := &middleware.Auth{DB: db}

	mux := http.NewServeMux()

	// CORS ミドルウェア
	mux.Handle("/", corsMiddleware(routes(h, auth)))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("starting server on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

func routes(h *handler.Handler, auth *middleware.Auth) http.Handler {
	mux := http.NewServeMux()

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

	// ヘルスチェック (DSR ロードバランサ用)
	mux.HandleFunc("GET /healthz", healthz(h))

	return mux
}

// healthz は DSR ロードバランサのヘルスチェック用エンドポイント。
//
// TODO: 中身が未実装のスタブ。現状はプロセスが生きていれば必ず 200 を返すため、
// infra/terraform/network.tf の TCP チェックと判定能力が変わらない。
// 少なくとも DB への疎通確認 (h.DB.PingContext) を入れて、DB に繋がらない
// ノードが 503 を返して LB から自動的に外れるようにすること。
// 実装したら network.tf の server ブロックを
// protocol = "http" / path = "/healthz" / status = 200 に切り替える。
func healthz(h *handler.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	allowedOrigin := os.Getenv("ALLOWED_ORIGIN")
	if allowedOrigin == "" {
		allowedOrigin = "http://localhost:3000"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
