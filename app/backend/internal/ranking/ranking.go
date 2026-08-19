// Package ranking は人気投稿のランキングを定期的に事前集計する。
//
// feed=recommended と /trending は、いいねを集計してから並べ替える必要があり、
// リクエストのたびに likes 全体を GROUP BY していた。この集計は索引では解消できないため、
// バックグラウンドで post_ranking に書き出し、API は順位で引くだけにする。
package ranking

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"
)

const (
	// LockName は集計の排他に使う MariaDB のアドバイザリロック名。
	// 本番は API が複数インスタンスで動くため、これで常に1台だけが集計するようにする。
	LockName = "post_ranking_refresh"

	// TopN は各時間窓で保持する順位の上限。
	// これを超えるページはハンドラ側で従来のクエリにフォールバックする。
	TopN = 1000

	// DefaultInterval は集計の実行間隔。ランキングは最大でこの分だけ古くなる。
	DefaultInterval = 5 * time.Minute

	// Window1h は /trending が、Window24h は feed=recommended が参照する時間窓。
	Window1h  = "1h"
	Window24h = "24h"
)

// window は1つの時間窓の集計条件。
// interval と orderBy は SQL に直接埋め込むため、外部入力を入れないこと。
type window struct {
	key      string
	interval string
	// rootOnly が true のとき返信を除く（recommended はタイムラインと同じ母集団にする）。
	rootOnly bool
	orderBy  string
}

var windows = []window{
	// /trending は返信も含め、同点は post_id で決める（従来クエリと同じ並び）。
	{key: Window1h, interval: "1 HOUR", rootOnly: false, orderBy: "score DESC, post_id DESC"},
	// feed=recommended は非返信のみで、同点は新しい投稿を優先（従来クエリと同じ並び）。
	{key: Window24h, interval: "24 HOUR", rootOnly: true, orderBy: "score DESC, created_at DESC, post_id DESC"},
}

// Refresher は post_ranking を定期的に更新する。
type Refresher struct {
	DB       *sql.DB
	Interval time.Duration
	TopN     int
}

func New(db *sql.DB) *Refresher {
	return &Refresher{DB: db, Interval: DefaultInterval, TopN: TopN}
}

// Start は集計ループを開始する。ctx がキャンセルされるまで戻らないため、goroutine で呼ぶ。
// 起動直後に1回実行し、以降は Interval ごとに繰り返す。
func (r *Refresher) Start(ctx context.Context) {
	r.runOnce(ctx)

	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runOnce(ctx)
		}
	}
}

func (r *Refresher) runOnce(ctx context.Context) {
	started := time.Now()
	ran, err := r.RefreshOnce(ctx)
	switch {
	case err != nil:
		log.Printf("ranking: refresh failed: %v", err)
	case !ran:
		log.Printf("ranking: skipped (another instance holds the lock)")
	default:
		log.Printf("ranking: refreshed in %s", time.Since(started).Round(time.Millisecond))
	}
}

// RefreshOnce はロックを取得できた場合だけ集計する。
// 取得できなければ他インスタンスが実行中なので (false, nil) を返す。
//
// GET_LOCK はコネクション単位のため、取得から解放まで同じコネクションで実行する必要がある。
// プールから任意のコネクションを借りると解放に失敗するので、sql.Conn で固定する。
func (r *Refresher) RefreshOnce(ctx context.Context) (bool, error) {
	conn, err := r.DB.Conn(ctx)
	if err != nil {
		return false, fmt.Errorf("acquire conn: %w", err)
	}
	defer conn.Close()

	var got sql.NullInt64
	// 第2引数の 0 は「取れなければ待たずに諦める」。
	if err := conn.QueryRowContext(ctx, `SELECT GET_LOCK(?, 0)`, LockName).Scan(&got); err != nil {
		return false, fmt.Errorf("get_lock: %w", err)
	}
	if !got.Valid || got.Int64 != 1 {
		return false, nil
	}
	defer func() {
		if _, err := conn.ExecContext(context.WithoutCancel(ctx), `SELECT RELEASE_LOCK(?)`, LockName); err != nil {
			log.Printf("ranking: release_lock failed: %v", err)
		}
	}()

	for _, w := range windows {
		if err := r.refreshWindow(ctx, conn, w); err != nil {
			return true, fmt.Errorf("refresh window %s: %w", w.key, err)
		}
	}
	return true, nil
}

// refreshWindow は1つの時間窓ぶんを入れ替える。
// 読み手が中途半端な状態を見ないよう、削除と挿入を1つのトランザクションにまとめる。
func (r *Refresher) refreshWindow(ctx context.Context, conn *sql.Conn, w window) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM post_ranking WHERE window_key = ?`, w.key); err != nil {
		return err
	}

	rootCond := ""
	if w.rootOnly {
		rootCond = "AND p.parent_post_id IS NULL"
	}

	// 内側でいいねを集約して上位だけに絞り、外側で順位を振る。
	// interval / orderBy / rootCond は上の windows で定義した定数のみ。
	query := fmt.Sprintf(`
		INSERT INTO post_ranking (window_key, rank_pos, post_id, score)
		SELECT ?, ROW_NUMBER() OVER (ORDER BY %s), t.post_id, t.score
		FROM (
			SELECT l.post_id AS post_id, COUNT(*) AS score, p.created_at AS created_at
			FROM likes l
			JOIN posts p ON p.id = l.post_id %s
			WHERE l.created_at > NOW() - INTERVAL %s
			GROUP BY l.post_id, p.created_at
			ORDER BY %s
			LIMIT ?
		) t
	`, w.orderBy, rootCond, w.interval, w.orderBy)

	if _, err := tx.ExecContext(ctx, query, w.key, r.TopN); err != nil {
		return err
	}
	return tx.Commit()
}
