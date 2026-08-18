# Sakuravel

Twitter/X ライクな SNS アプリケーション「Sakuravel」。パフォーマンスチューニング演習用のリポジトリです。**意図的にチューニング前の状態**で実装されているので、計測しながら速くしていってください。

チューニング対象はバックエンド（`backend/`）です。フロントエンドはビルド済みのコンテナイメージを配布しているため、ソースコードは含まれていません。

## 構成

```
.
└── backend/    Go + MariaDB の REST API（チューニング対象）
    └── docs/   API 仕様・DB 設計・各種手順
```

## クイックスタート

### 事前準備: フロントエンドのイメージ名を設定する

`backend/docker-compose.yml` の `frontend` サービスは、イメージ名がプレースホルダのままです。
**ここを置き換えないと起動できません**（`invalid reference format` で失敗します）。

```yaml
  frontend:
    image: <作成したコンテナレジストリのホスト名>/intern2026-app-frontend:latest
```

フロントエンドはソースコードを含まないため、配布用イメージを利用します。
`<作成したコンテナレジストリのホスト名>` を、配布元のホスト名（下記「コンテナレジストリ」を参照）か、
自分のレジストリに push し直した場合はそのホスト名に書き換えてください。
自分のレジストリへ移す手順は [`backend/docs/説明資料/registry.md`](./backend/docs/説明資料/registry.md) にあります。

### まとめて起動

イメージ名を設定したら、`backend` ディレクトリで:

```bash
cd backend
docker compose up -d
```

フロントエンド・バックエンド（API + MariaDB）の3サービスがすべて Docker でバックグラウンド起動します。

- フロントエンド: `http://localhost:3000`
- API: `http://localhost:8080`
- MariaDB: `localhost:3306`

| 操作 | コマンド |
|---|---|
| 停止（削除はしない） | `docker compose stop` |
| 再起動 | `docker compose restart` |
| 停止＋削除（DBのデータはボリュームに残る） | `docker compose down` |
| ログを追う | `docker compose logs -f` |
| DBを含めて完全に作り直す | `docker compose down -v` |

`stop` / `restart` / `logs` はサービス名（`frontend` / `api` / `db`）を渡すと個別に操作できます。

`down -v` は DB のボリュームごと削除するため、次回起動時に `backend/migrations/` が再実行されてスキーマが作り直されます。投入したダミーデータも消えるので、必要なら再度シードを流してください。

個別に起動したい場合は以下の手順です。

### 1. バックエンドを起動

```bash
cd backend
docker compose up -d
```

- API: `http://localhost:8080`
- MariaDB: `localhost:3306`（DB: `sakuravel` / user: `sakuravel` / password: `password`）

初回起動時のみ `migrations/*.sql` が自動実行され、スキーマが作成されます。

### 2. ダミーデータを投入（任意）

パフォーマンス課題として重いデータ量を体感したい場合、シードスクリプトを実行します（数十分かかります）。

`backend` ディレクトリで以下を実行します。

```bash
docker run --rm \
  -v "$(pwd):/app" \
  -v gomodcache:/go/pkg/mod \
  -w /app \
  -e DATABASE_URL="sakuravel:password@tcp(db:3306)/sakuravel?parseTime=true&charset=utf8mb4" \
  --network app-network \
  golang:1.25-alpine go run ./seed/main.go -scale 100
```

`-scale` を省略するとデフォルトの1/100の件数（users 500 / posts 10,000 など）になり、動作確認用としてすぐ終わります。
ローカルに Go がある場合は `backend/` で `go run ./seed/main.go` を直接叩いても構いません。詳細は [`backend/docs/説明資料/ダミーデータの投入手順.md`](./backend/docs/説明資料/ダミーデータの投入手順.md) を参照。

投入されるデータ量（`-scale 100` 指定時）:

| テーブル | 件数 |
|---|---|
| users | 50,000 |
| posts（通常投稿） | 1,000,000 |
| posts（返信） | 約 540,000 |
| follows | 5,000,000 |
| likes | 10,000,000 |
| reposts | 2,000,000 |
| footprints | 2,000,000 |
| notifications | 2,000,000 |
| notifications（`reply`） | 約 540,000 |

シードユーザーは `user00001@example.com` 〜 `user50000@example.com`、パスワードは全員 `password` です。

### 3. フロントエンドを起動

フロントエンドはビルド済みイメージを使うため、個別のセットアップは不要です（`docker compose up -d` で一緒に起動します）。
`http://localhost:3000` でアプリにアクセスできます。

## 技術スタック

**バックエンド**
- Go 1.25 / 標準 `net/http`（Go 1.22+ の `ServeMux` によるルーティング）
- MariaDB 10.11（`go-sql-driver/mysql` ドライバ）
- Cookieベースのセッション認証

**フロントエンド**
- Next.js 16 / React 19
- Tailwind CSS v4
- TypeScript

## HTTPS で公開する

`backend/proxy/` に nginx（TLS 終端）の設定があります。フロントエンドとバックエンドを
**同一オリジン**に載せる構成です。

```
https://<host>/           → frontend:3000
https://<host>/backend/*  → api:8080   （nginx が /backend を除去して渡す）
```

別オリジンに分けないのは、この構成だと

- CORS が不要になる（別オリジンだと GET を含む**全リクエストがプリフライト対象**になり往復が倍になります）
- セッションCookieがサードパーティCookie扱いにならない（Safari 等でブロックされない）

ためです。ブラウザの「1オリジン6接続」制限に SSE が引っかからないよう、**HTTP/2 を有効にしてあります**。

### ローカルで HTTPS を確認する

```bash
cd backend
./scripts/gen-dev-cert.sh                                     # 自己署名証明書を作る
docker compose -f docker-compose.yml -f compose.https.yml up -d
```

`https://localhost/` にアクセスします（自己署名なのでブラウザの警告を承認してください）。
素の `docker compose up -d` は今まで通り HTTP のままです。

### 本番（グローバルIPを持つノード）

ホスト名には `<グローバルIP>.sslip.io` を使います（DNS の設定は不要です）。

1. `compose.reg.yml` の `frontend.API_URL` を `https://<グローバルIP>.sslip.io/backend` に書き換える
2. 証明書を発行する（**まず `--staging` を付けて一度通してから**本番発行します）

```bash
./scripts/init-letsencrypt.sh -d <グローバルIP>.sslip.io -m you@example.com --staging
./scripts/init-letsencrypt.sh -d <グローバルIP>.sslip.io -m you@example.com
```

3. `docker compose -f compose.reg.yml up -d`

証明書の有効期限は90日です。更新には 80 番の開放が必要なので閉じないでください。

## コンテナレジストリ

本番・配布用の Docker イメージは以下のコンテナレジストリを利用します。

- バックエンド: `intern22.sakuracr.jp/intern2026-app-backend:latest`
  - デフォルトは8080でlistenします
  - [Dockerfile](./backend/Dockerfile)
- フロントエンド: `intern22.sakuracr.jp/intern2026-app-frontend:latest`
  - 3000でlistenします
  - ビルド済みイメージを配布しています（ソースコードは非公開）

※ レジストリがあるクラウドプロジェクトの認証情報は1Passwordを参照
## 環境変数

### バックエンドAPIで利用される環境変数

実行時に以下の値を読み込みます。

| 変数名 | 説明 | デフォルト値 |
|---|---|---|
| `DATABASE_URL` | MariaDB 接続文字列 | 空文字 |
| `PORT` | API サーバーがリッスンするポート | `8080` |
| `ALLOWED_ORIGIN` | CORS で許可するフロントエンドのオリジン。**未設定なら CORS ヘッダを一切付けない**（リバースプロキシでフロントと同一オリジンに載せる構成用） | 空文字（CORS 無効） |
| `COOKIE_SECURE` | `true` にするとセッションCookieに `Secure` を付与する。HTTPS 配信時は必須 | `false` |
| `COOKIE_SAMESITE` | セッションCookieの `SameSite` 属性。`lax` / `none` / `strict`。`none` はフロントと API が別オリジンになる構成でのみ使う（`Secure` が前提） | `lax` |

※ PORTを変更する場合は下記の`API_URL`も変更する必要があります

### フロントエンドの環境変数（実行時に読み込み）

| 変数名 | 説明 | デフォルト値 |
|---|---|---|
| `API_URL` | ブラウザから呼び出すバックエンド URL。`NEXT_PUBLIC_*` ではないため `next build` 時には埋め込まれず、コンテナ起動時の値がそのまま `/api/config` 経由でブラウザに渡される | `http://localhost:8080` |

イメージは一度ビルドすれば環境ごとの再ビルドは不要です。デプロイ先ごとに `API_URL` の値だけ変えて起動してください。  
`API_URL` にはパスを含められます（例: `https://example.com/backend`）。リバースプロキシでフロントと同一オリジンに載せる場合はこの形にします。

フロントエンドとバックエンドを**別オリジン**に分ける構成（さくらのAppRun専有型など）では、バックエンド側に `ALLOWED_ORIGIN` と `COOKIE_SECURE=true` と `COOKIE_SAMESITE=none` を併せて設定してください。

## API エンドポイント

| カテゴリ | メソッド・パス | 概要 |
|---|---|---|
| 認証 | `POST /register` | ユーザー登録 |
| | `POST /login` | ログイン |
| | `POST /logout` | ログアウト |
| | `GET /me` | 自分の情報取得 |
| 足跡 | `GET /me/footprints` | プロフィール訪問者一覧 |
| ユーザー | `GET /profile/{user_id}` | プロフィール取得 |
| | `PUT /profile` | プロフィール更新 |
| | `GET /users/{user_id}/followers` | フォロワー一覧 |
| | `GET /users/{user_id}/following` | フォロー中一覧 |
| | `POST /users/{user_id}/follow` | フォロー |
| | `DELETE /users/{user_id}/follow` | フォロー解除 |
| 投稿 | `GET /posts` | タイムライン取得 |
| | `POST /posts` | 投稿作成 |
| | `GET /posts/{id}` | 投稿取得 |
| | `DELETE /posts/{id}` | 投稿削除 |
| | `GET /users/{user_id}/posts` | ユーザーの投稿一覧（`type=posts`/`replies`） |
| 返信（スレッド） | `POST /replies` | 返信作成 |
| | `GET /posts/{id}/thread` | スレッド取得（祖先＋対象＋返信ツリー） |
| | `GET /posts/{id}/thread/stream` | スレッドへの新規返信を SSE 配信 |
| いいね | `GET /posts/{id}/likes` | いいねしたユーザー一覧 |
| | `POST /likes` | いいね |
| | `DELETE /likes/{post_id}` | いいね解除 |
| リポスト | `POST /reposts` | リポスト |
| | `DELETE /reposts/{post_id}` | リポスト解除 |
| 検索 | `GET /search` | 投稿・ユーザー検索 |
| 通知 | `GET /notifications` | 通知一覧 |
| | `POST /notifications/read` | 既読化 |
| | `GET /notifications/unread_count` | 未読件数 |
| | `GET /notifications/stream` | 通知を SSE 配信 |
| トレンド | `GET /trending` | トレンド投稿 |

## パフォーマンスチューニング課題

このリポジトリには、性能上の問題が複数仕込まれています。どこが遅いのかは計測して見つけてください。

アプリの仕様は [`backend/README.md`](./backend/README.md) と [`backend/docs/`](./backend/docs/) を参照してください。

## デプロイについて
- デプロイ手順は別途共有します
