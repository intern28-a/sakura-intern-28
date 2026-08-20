// Package ranking は人気投稿ランキングの定義を持つ。
//
// feed=recommended と /trending は、いいねを集計してから並べ替える必要があり、
// リクエストのたびに likes 全体を GROUP BY していた。この集計は索引では解消できないため、
// post_ranking テーブルに事前集計した結果を持ち、API は順位で引くだけにしている。
//
// 集計そのものは MariaDB の EVENT が行う（migrations/005_post_ranking_event.sql）。
// EVENT はサーバー単位で1回しか走らないため、API を複数インスタンスで動かしても
// 重複して集計されることはない。ここにあるのは、その SQL と共有する定数だけ。
package ranking

const (
	// TopN は各時間窓で post_ranking が保持する順位の上限。
	// EVENT 側の LIMIT と一致させること。これを超えるページは
	// ハンドラがその場で集約するクエリにフォールバックする。
	TopN = 1000

	// Window1h は /trending が、Window24h は feed=recommended が参照する時間窓。
	// post_ranking.window_key に入る値。
	Window1h  = "1h"
	Window24h = "24h"
)
