package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

const (
	numUsers         = 9999
	numPosts         = 100000
	numFollows       = 500000
	numLikes         = 10000000
	numReposts       = 200000
	numFootprints    = 200000
	numNotifications = 200000
	// 1 回の INSERT で扱う行数は大きくするが、MySQL のプレースホルダー上限を超えないよう
	// その都度 rowsPerInsert を計算して制限する。
	batchSize = 50000

	// MySQL の prepared statement は 1 クエリあたりのプレースホルダー数に上限がある。
	// ここでは安全マージンを残したうえで、各 INSERT の行数を自動調整する。
	maxMySQLPlaceholders = 65535
	placeholderHeadroom  = 2048

	// 返信が付く投稿の割合
	replyTargetRatio = 0.2
	// 返信にさらに返信が付く割合（これを繰り返すことで深さがランダムになる）
	replyContinueRatio = 0.25
	// 返信をネストさせる深さの上限
	maxReplyDepth = 6
)

var sampleTexts = []string{
	"さくらのクラウドは使いやすいですね！",
	"今日も元気にコーディング中 💻",
	"データベースのインデックスって大事だなと実感",
	"Go言語の並行処理、面白い",
	"新しい機能をリリースしました！",
	"バグを直したと思ったら別のバグが出た",
	"コードレビューありがとうございます",
	"テストを書く習慣をつけたい",
	"インターンシップ楽しい！",
	"クラウドインフラの勉強中",
	"チームランチ最高でした 🍜",
	"OSS活動始めてみようかな",
	"Dockerって便利だなあ",
	"CI/CDパイプライン整備完了",
	"コードの可読性を意識してみる",
	"アーキテクチャ設計って難しい",
	"なんとか本番リリース成功！",
	"ドキュメント整備大事",
	"ペアプロで学びが深まった",
	"エラーログを読む力をつけたい",
}

func main() {
	scale := flag.Int("scale", 1, "データ件数の倍率")
	flag.Parse()

	users := numUsers * *scale
	posts := numPosts * *scale
	follows := numFollows * *scale
	likes := numLikes * *scale
	reposts := numReposts * *scale
	footprints := numFootprints * *scale
	notifications := numNotifications * *scale

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatalf("DATABASE_URL is not set")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal("DB接続失敗:", err)
	}

	rng := rand.New(rand.NewSource(42))

	log.Println("=== シードデータ生成開始 ===")
	start := time.Now()

	log.Printf("ユーザー %d 件 作成中...", users)
	userIDs := insertUsers(db, rng, users)

	log.Printf("投稿 %d 件 作成中...", posts)
	postIDs := insertPosts(db, rng, userIDs, posts)

	log.Printf("返信 作成中（投稿の %.0f%% に返信、深さは最大 %d）...", replyTargetRatio*100, maxReplyDepth)
	replyIDs := insertReplies(db, rng, userIDs, postIDs)
	log.Printf("  返信 %d 件 作成完了", len(replyIDs))

	log.Printf("フォロー %d 件 作成中...", follows)
	insertFollows(db, rng, userIDs, follows)

	log.Printf("いいね %d 件 作成中...", likes)
	insertLikes(db, rng, userIDs, postIDs, likes)

	log.Printf("リポスト %d 件 作成中...", reposts)
	insertReposts(db, rng, userIDs, postIDs, reposts)

	log.Printf("足跡 %d 件 作成中...", footprints)
	insertFootprints(db, rng, userIDs, footprints)

	log.Printf("通知 %d 件 作成中...", notifications)
	insertNotifications(db, rng, userIDs, postIDs, notifications)

	// 返信通知は他の通知より後に入れる（通知一覧で埋もれないようにするため）
	log.Println("返信通知 作成中...")
	log.Printf("  返信通知 %d 件 作成完了", insertReplyNotifications(db))

	log.Printf("=== 完了 (%.1f秒) ===", time.Since(start).Seconds())
}

func maxRowsPerInsert(columns int) int {
	if columns <= 0 {
		return batchSize
	}
	limit := maxMySQLPlaceholders - placeholderHeadroom
	rows := limit / columns
	if rows < 1 {
		return 1
	}
	if rows > batchSize {
		return batchSize
	}
	return rows
}

func insertUsers(db *sql.DB, rng *rand.Rand, n int) []int64 {
	hash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	hashStr := string(hash)

	ids := make([]int64, 0, n)
	rowsPerInsert := maxRowsPerInsert(5)
	for i := 0; i < n; i += rowsPerInsert {
		end := i + rowsPerInsert
		if end > n {
			end = n
		}

		tx, err := db.Begin()
		if err != nil {
			log.Fatal("users begin:", err)
		}

		vals := make([]string, 0, end-i)
		args := make([]any, 0, (end-i)*5)
		for j := i; j < end; j++ {
			username := fmt.Sprintf("user%05d", j+1)
			displayName := fmt.Sprintf("ユーザー%d", j+1)
			bio := fmt.Sprintf("%dのプロフィール文です", j+1)
			email := fmt.Sprintf("user%05d@example.com", j+1)
			vals = append(vals, "(?,?,?,?,?)")
			args = append(args, username, email, hashStr, displayName, bio)
		}

		query := "INSERT INTO users (username, email, password_hash, display_name, bio) VALUES " +
			strings.Join(vals, ",")
		res, err := tx.Exec(query, args...)
		if err != nil {
			tx.Rollback()
			log.Fatal("users insert:", err)
		}
		firstID, err := res.LastInsertId()
		if err != nil {
			tx.Rollback()
			log.Fatal("users last insert id:", err)
		}
		rowsAffected, _ := res.RowsAffected()
		if err := tx.Commit(); err != nil {
			log.Fatal("users commit:", err)
		}

		for k := int64(0); k < rowsAffected; k++ {
			ids = append(ids, firstID+k)
		}

		if (i/batchSize+1)%10 == 0 {
			log.Printf("  users: %d/%d", end, n)
		}
	}
	return ids
}

func insertPosts(db *sql.DB, rng *rand.Rand, userIDs []int64, n int) []int64 {
	ids := make([]int64, 0, n)
	rowsPerInsert := maxRowsPerInsert(2)
	for i := 0; i < n; i += rowsPerInsert {
		end := i + rowsPerInsert
		if end > n {
			end = n
		}

		tx, err := db.Begin()
		if err != nil {
			log.Fatal("posts begin:", err)
		}

		vals := make([]string, 0, end-i)
		args := make([]any, 0, (end-i)*2)
		for j := i; j < end; j++ {
			uid := userIDs[rng.Intn(len(userIDs))]
			text := sampleTexts[rng.Intn(len(sampleTexts))]
			vals = append(vals, "(?,?)")
			args = append(args, uid, text)
		}

		query := "INSERT INTO posts (user_id, content) VALUES " +
			strings.Join(vals, ",")
		res, err := tx.Exec(query, args...)
		if err != nil {
			tx.Rollback()
			log.Fatal("posts insert:", err)
		}
		firstID, err := res.LastInsertId()
		if err != nil {
			tx.Rollback()
			log.Fatal("posts last insert id:", err)
		}
		rowsAffected, _ := res.RowsAffected()
		if err := tx.Commit(); err != nil {
			log.Fatal("posts commit:", err)
		}

		for k := int64(0); k < rowsAffected; k++ {
			ids = append(ids, firstID+k)
		}

		if (i/batchSize+1)%20 == 0 {
			log.Printf("  posts: %d/%d", end, n)
		}
	}
	return ids
}

// insertReplies は投稿の一定割合に返信を付ける。作成した返信にもさらに一定確率で
// 返信を付ける、というのを深さの上限まで繰り返すことで深さがまちまちなスレッドを作る。
func insertReplies(db *sql.DB, rng *rand.Rand, userIDs, postIDs []int64) []int64 {
	all := make([]int64, 0)

	parents := make([]int64, 0, len(postIDs))
	for _, id := range postIDs {
		if rng.Float64() < replyTargetRatio {
			parents = append(parents, id)
		}
	}

	for depth := 1; depth <= maxReplyDepth && len(parents) > 0; depth++ {
		// 1つの投稿に複数の返信がぶら下がるよう、返信先を件数分だけ繰り返す
		targets := make([]int64, 0, len(parents)*2)
		for _, id := range parents {
			for k := replyFanout(rng); k > 0; k-- {
				targets = append(targets, id)
			}
		}

		created := insertReplyBatch(db, rng, userIDs, targets)
		all = append(all, created...)
		log.Printf("  replies: 深さ%d に %d 件", depth, len(created))

		next := make([]int64, 0, len(created))
		for _, id := range created {
			if rng.Float64() < replyContinueRatio {
				next = append(next, id)
			}
		}
		parents = next
	}
	return all
}

// insertReplyNotifications は作成済みの返信から reply 型の通知を作る。
// 宛先は返信先の投稿者。自分の投稿への自己返信は除外する。
func insertReplyNotifications(db *sql.DB) int64 {
	res, err := db.Exec(`
		INSERT INTO notifications (user_id, type, actor_id, post_id)
		SELECT parent.user_id, 'reply', child.user_id, child.id
		FROM posts child
		JOIN posts parent ON parent.id = child.parent_post_id
		WHERE child.parent_post_id IS NOT NULL
		  AND parent.user_id <> child.user_id
	`)
	if err != nil {
		log.Fatal("reply notifications insert:", err)
	}
	n, _ := res.RowsAffected()
	return n
}

// replyFanout は1つの投稿にぶら下がる直接の返信数を返す（1〜4件で重み付け）。
func replyFanout(rng *rand.Rand) int {
	switch r := rng.Float64(); {
	case r < 0.55:
		return 1
	case r < 0.85:
		return 2
	case r < 0.96:
		return 3
	default:
		return 4
	}
}

// insertReplyBatch は targets の各投稿に返信を1件ずつ作成し、作成された投稿IDを返す。
// 同じ投稿IDが複数回含まれていれば、その数だけ返信がぶら下がる。
func insertReplyBatch(db *sql.DB, rng *rand.Rand, userIDs, targets []int64) []int64 {
	ids := make([]int64, 0, len(targets))
	rowsPerInsert := maxRowsPerInsert(3)
	for i := 0; i < len(targets); i += rowsPerInsert {
		end := i + rowsPerInsert
		if end > len(targets) {
			end = len(targets)
		}

		tx, err := db.Begin()
		if err != nil {
			log.Fatal("replies begin:", err)
		}

		vals := make([]string, 0, end-i)
		args := make([]any, 0, (end-i)*3)
		for j := i; j < end; j++ {
			uid := userIDs[rng.Intn(len(userIDs))]
			text := sampleTexts[rng.Intn(len(sampleTexts))]
			vals = append(vals, "(?,?,?)")
			args = append(args, uid, text, targets[j])
		}

		query := "INSERT INTO posts (user_id, content, parent_post_id) VALUES " +
			strings.Join(vals, ",")
		res, err := tx.Exec(query, args...)
		if err != nil {
			tx.Rollback()
			log.Fatal("replies insert:", err)
		}
		firstID, err := res.LastInsertId()
		if err != nil {
			tx.Rollback()
			log.Fatal("replies last insert id:", err)
		}
		rowsAffected, _ := res.RowsAffected()
		if err := tx.Commit(); err != nil {
			log.Fatal("replies commit:", err)
		}

		for k := int64(0); k < rowsAffected; k++ {
			ids = append(ids, firstID+k)
		}
	}
	return ids
}

func insertFollows(db *sql.DB, rng *rand.Rand, userIDs []int64, n int) {
	seen := make(map[[2]int64]bool, n)
	inserted := 0
	rowsPerInsert := maxRowsPerInsert(2)

	for inserted < n {
		batchEnd := inserted + rowsPerInsert
		if batchEnd > n {
			batchEnd = n
		}
		count := batchEnd - inserted

		vals := make([]string, 0, count)
		args := make([]any, 0, count*2)
		added := 0
		for added < count {
			follower := userIDs[rng.Intn(len(userIDs))]
			followee := userIDs[rng.Intn(len(userIDs))]
			if follower == followee {
				continue
			}
			key := [2]int64{follower, followee}
			if seen[key] {
				continue
			}
			seen[key] = true
			vals = append(vals, "(?,?)")
			args = append(args, follower, followee)
			added++
		}

		query := "INSERT INTO follows (follower_id, followee_id) VALUES " +
			strings.Join(vals, ",") +
			" ON DUPLICATE KEY UPDATE follower_id = follower_id"
		if _, err := db.Exec(query, args...); err != nil {
			log.Fatal("follows insert:", err)
		}
		inserted += added
		if inserted%(batchSize*10) == 0 {
			log.Printf("  follows: %d/%d", inserted, n)
		}
	}
}

func insertLikes(db *sql.DB, rng *rand.Rand, userIDs, postIDs []int64, n int) {
	if len(userIDs) == 0 || len(postIDs) == 0 {
		return
	}
	if n > len(userIDs)*len(postIDs) {
		n = len(userIDs) * len(postIDs)
	}

	inserted := 0
	rowsPerInsert := maxRowsPerInsert(2)

	for inserted < n {
		batchEnd := inserted + rowsPerInsert
		if batchEnd > n {
			batchEnd = n
		}
		count := batchEnd - inserted

		vals := make([]string, 0, count)
		args := make([]any, 0, count*2)
		for j := 0; j < count; j++ {
			pairIndex := inserted + j
			uidIdx := pairIndex / len(postIDs)
			pidIdx := pairIndex % len(postIDs)
			uid := userIDs[uidIdx]
			pid := postIDs[pidIdx]
			vals = append(vals, "(?,?)")
			args = append(args, uid, pid)
		}

		query := "INSERT INTO likes (user_id, post_id) VALUES " +
			strings.Join(vals, ",") +
			" ON DUPLICATE KEY UPDATE user_id = user_id"
		if _, err := db.Exec(query, args...); err != nil {
			log.Fatal("likes insert:", err)
		}
		inserted += count
		if inserted%(batchSize*20) == 0 {
			log.Printf("  likes: %d/%d", inserted, n)
		}
	}
}

func insertReposts(db *sql.DB, rng *rand.Rand, userIDs, postIDs []int64, n int) {
	if len(userIDs) == 0 || len(postIDs) == 0 {
		return
	}
	if n > len(userIDs)*len(postIDs) {
		n = len(userIDs) * len(postIDs)
	}

	inserted := 0
	rowsPerInsert := maxRowsPerInsert(2)

	for inserted < n {
		batchEnd := inserted + rowsPerInsert
		if batchEnd > n {
			batchEnd = n
		}
		count := batchEnd - inserted

		vals := make([]string, 0, count)
		args := make([]any, 0, count*2)
		for j := 0; j < count; j++ {
			pairIndex := inserted + j
			uidIdx := pairIndex / len(postIDs)
			pidIdx := pairIndex % len(postIDs)
			uid := userIDs[uidIdx]
			pid := postIDs[pidIdx]
			vals = append(vals, "(?,?)")
			args = append(args, uid, pid)
		}

		query := "INSERT INTO reposts (user_id, post_id) VALUES " +
			strings.Join(vals, ",") +
			" ON DUPLICATE KEY UPDATE user_id = user_id"
		if _, err := db.Exec(query, args...); err != nil {
			log.Fatal("reposts insert:", err)
		}
		inserted += count
		if inserted%(batchSize*10) == 0 {
			log.Printf("  reposts: %d/%d", inserted, n)
		}
	}
}

// pickOther は base とは別のユーザーIDを返す。
// 自分自身への足跡・通知は作らないため。
func pickOther(rng *rand.Rand, userIDs []int64, base int64) int64 {
	if len(userIDs) < 2 {
		return base
	}
	for {
		id := userIDs[rng.Intn(len(userIDs))]
		if id != base {
			return id
		}
	}
}

func insertFootprints(db *sql.DB, rng *rand.Rand, userIDs []int64, n int) {
	inserted := 0
	rowsPerInsert := maxRowsPerInsert(2)
	for inserted < n {
		end := inserted + rowsPerInsert
		if end > n {
			end = n
		}
		count := end - inserted

		vals := make([]string, 0, count)
		args := make([]any, 0, count*2)
		for j := 0; j < count; j++ {
			uid := userIDs[rng.Intn(len(userIDs))]
			vid := pickOther(rng, userIDs, uid)
			vals = append(vals, "(?,?)")
			args = append(args, uid, vid)
		}

		query := "INSERT INTO footprints (user_id, visitor_id) VALUES " + strings.Join(vals, ",")
		if _, err := db.Exec(query, args...); err != nil {
			log.Fatal("footprints insert:", err)
		}
		inserted += count
		if inserted%(batchSize*10) == 0 {
			log.Printf("  footprints: %d/%d", inserted, n)
		}
	}
}

var notifTypes = []string{"like", "follow", "repost", "footprint"}

func insertNotifications(db *sql.DB, rng *rand.Rand, userIDs, postIDs []int64, n int) {
	inserted := 0
	rowsPerInsert := maxRowsPerInsert(4)
	for inserted < n {
		end := inserted + rowsPerInsert
		if end > n {
			end = n
		}
		count := end - inserted

		vals := make([]string, 0, count)
		args := make([]any, 0, count*4)
		for j := 0; j < count; j++ {
			uid := userIDs[rng.Intn(len(userIDs))]
			actorID := pickOther(rng, userIDs, uid)
			ntype := notifTypes[rng.Intn(len(notifTypes))]
			var postID *int64
			if ntype == "like" || ntype == "repost" {
				pid := postIDs[rng.Intn(len(postIDs))]
				postID = &pid
			}
			vals = append(vals, "(?,?,?,?)")
			args = append(args, uid, ntype, actorID, postID)
		}

		query := "INSERT INTO notifications (user_id, type, actor_id, post_id) VALUES " + strings.Join(vals, ",")
		if _, err := db.Exec(query, args...); err != nil {
			log.Fatal("notifications insert:", err)
		}
		inserted += count
		if inserted%(batchSize*10) == 0 {
			log.Printf("  notifications: %d/%d", inserted, n)
		}
	}
}
