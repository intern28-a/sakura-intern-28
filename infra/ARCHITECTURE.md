# インフラ構成

さくらのクラウド `tk1a` ゾーン上に Terraform で構築している。このドキュメントは `infra/terraform` の実際の定義から起こしたもので、値はすべて変数のデフォルト値。

グローバルIPはさくら側から払い出されるため、実際の値は `terraform output` で確認する（`pub_network` / `node_public_ips` / `lb_frontend` / `lb_api`）。以下では `203.0.113.0/28` を例として使う。

## 全体図

```mermaid
graph TB
    client["インターネット<br/>クライアント (ブラウザ)"]

    subgraph shared["共有セグメント"]
        edge_pub["edge<br/>グローバルIP"]
    end

    subgraph pub["ルータ+スイッチ / 203.0.113.0/28"]
        lba["<b>LB-A</b> .4<br/>VIP .5:3000"]
        lbb["<b>LB-B</b> .6<br/>VIP .7:8080"]
        n2p["node-02 .8"]
        n3p["node-03 .9"]
        n4p["node-04 .10"]
        n5p["node-05 .11"]
    end

    subgraph app["app スイッチ / 192.168.1.0/24 (ルータなし)"]
        edge["<b>edge</b> .11<br/>Ansible コントローラ + 踏み台"]
        n2["node-02 .12"]
        n3["node-03 .13"]
        n4["node-04 .14"]
        n5["node-05 .15"]
        db[("MariaDB 10.11<br/>db .30:3306")]
    end

    client -->|"HTML/JS :3000"| lba
    client -->|"API :8080"| lbb

    lba -->|"宛先MACのみ書換"| n2p
    lba --> n3p
    lbb --> n4p
    lbb --> n5p

    n2p -.->|"戻りは LB を通らず直接返す (DSR)"| client
    n4p -.-> client

    n2p -.->|"同一ホスト"| n2
    n3p -.-> n3
    n4p -.-> n4
    n5p -.-> n5

    n4 --> db
    n5 --> db

    edge_pub -.->|"同一ホスト"| edge
    edge -->|"SSH (ProxyJump)"| n2
```

テキスト版:

```
                        インターネット (ブラウザ)
                              │
              :3000 ──────────┴────────── :8080
                │                            │
         ┌──────┴──────┐              ┌──────┴──────┐
         │    LB-A     │              │    LB-B     │
         │  本体 .4    │              │  本体 .6    │
         │  VIP  .5    │              │  VIP  .7    │
         └──────┬──────┘              └──────┬──────┘
                │ 実サーバ                    │ 実サーバ
       ┌────────┴────────┐          ┌────────┴────────┐
       │                 │          │                 │
   ┌───┴────┐       ┌────┴───┐  ┌───┴────┐       ┌────┴───┐
   │node-02 │       │node-03 │  │node-04 │       │node-05 │
   │ .8/.12 │       │ .9/.13 │  │.10/.14 │       │.11/.15 │
   │frontend│       │frontend│  │  api   │       │  api   │
   └───┬────┘       └────┬───┘  └───┬────┘       └────┬───┘
       └─────────────────┴─────┬────┴─────────────────┘
                               │ app スイッチ (192.168.1.0/24) ※ルータなし
                   ┌───────────┴───────────┐
              ┌────┴─────┐            ┌────┴────┐
              │   edge   │            │   db    │
              │   .11    │            │   .30   │
              │ Ansible  │            │ MariaDB │
              │ + 踏み台 │            └─────────┘
              └──────────┘
              共有セグメントにもう1本 NIC を持つ
```

各ノードは NIC を 2 本持つ（接続順 = OS の enumeration 順）。

| | NIC[0] | NIC[1] |
|---|---|---|
| edge | 共有セグメント（DHCP、デフォルトルート） | app スイッチ（静的、経路なし） |
| node-02〜05 | ルータ+スイッチ（静的、デフォルトルート） | app スイッチ（静的、経路なし） |

**グローバル側は必ず NIC[0] に置く必要がある。** 2本目以降に接続しようとすると
API が `Only the first interface can be connected to router+switches or shared segments`
を返して作成に失敗する。cloud-init と Ansible はこの順序を前提に NIC を判別している。

## リソース一覧

| リソース | Terraform | 名前 | スペック |
|---|---|---|---|
| スイッチ | `sakura_vswitch.app` | `app-sw` | 192.168.1.0/24 |
| ルータ+スイッチ | `sakura_internet.pub` | `pub-router` | /28 · 100 Mbps |
| サーバ ×5 | `sakura_server.node[0..4]` | `edge`, `node-02`〜`05` | 4 core / 12 GB |
| ディスク ×5 | `sakura_disk.node[0..4]` | `edge-disk`, `node-02-disk`〜 | HDD 100 GB |
| ロードバランサ A | `sakura_dsr_lb.frontend` | `lb-frontend` | standard / DSR / VRID 1 |
| ロードバランサ B | `sakura_dsr_lb.api` | `lb-api` | standard / DSR / VRID 2 |
| パケットフィルタ ×2 | `sakura_packet_filter.node` | `pf-frontend`, `pf-api` | グローバル側NICに適用 |
| データベース | `sakura_database.db` | `db` | MariaDB 10.11 / 10g |
| SSH 公開鍵 | `sakura_ssh_key.foobar` | `localsshkey` | `~/.ssh/intern28.pub` |

ゾーンは全て `tk1a`。OS は Ubuntu Server 24.04.2 LTS (cloudimg)。スイッチの上限は 3 本なので、app スイッチ + ルータ+スイッチ の 2 本で収まっている。

## IP 割り当て

### app セグメント `192.168.1.0/24`

| アドレス | 用途 | 由来 |
|---|---|---|
| .11 | edge | `node_ip_offset = 11` |
| .12 〜 .15 | node-02 〜 node-05 | 同上 + インデックス |
| .30 | データベースアプライアンス | `db_ip_offset = 30` |

ルータが無いセグメントで、DB への到達にのみ使う。デフォルトルートはここに置かない。

### ルータ+スイッチ `203.0.113.0/28`（例）

`sakura_internet.pub.ip_addresses`（払い出し可能アドレスの昇順リスト）の先頭から順に取る。

| index | 用途 |
|---|---|
| 0 | LB-A 本体 |
| 1 | **VIP-A**（frontend の受け口 :3000） |
| 2 | LB-B 本体 |
| 3 | **VIP-B**（api の受け口 :8080） |
| 4 〜 7 | node-02 〜 node-05 |

edge はここには載らず、従来通り共有セグメントのグローバルIPを持つ。

## ノードの役割

### edge（旧 node-01）

**Ansible コントローラ兼踏み台**。アプリケーションのトラフィックは一切通らない。

| 役割 | 実装 |
|---|---|
| Ansible コントローラ | `infra/ansible/bootstrap.yml` がここに `ansible-core` と playbook 一式を配置し、`site.yml` をここから実行する |
| 踏み台 | node-02〜05 はパケットフィルタで 22 番を閉じてあるため、SSH は必ずここを経由する |

以前は NAT ゲートウェイと DNAT 入口も兼ねていたが、node-02〜05 がルータ+スイッチ側に自前のデフォルトルートを持つようになったため不要になり、撤去した。

LB の実サーバには含まれない。

### node-02 / node-03 — frontend

`frontend` コンテナ（:3000）だけを動かす。LB-A の実サーバ。

### node-04 / node-05 — api

`api` コンテナ（:8080）だけを動かす。LB-B の実サーバ。DB への接続は app スイッチ側 NIC を使う。

### DSR 方式に伴う共通設定

両ロールとも、以下が設定されている（新規ノードは cloud-init、既存ノードは `infra/ansible/site.yml`）。

- **lo に自分の役割の VIP を /32 で割り当て**（`dsr-vip.service`）— 宛先 IP が VIP のまま届くので、自分のアドレスとして持っていないと破棄される。加えて Docker の PREROUTING は `--dst-type LOCAL` で条件付けされているため、VIP がローカル扱いにならないと公開ポートの DNAT が発火しない
- **`arp_ignore=1` / `arp_announce=2`** — 抑止しないと lo の VIP に対する ARP へ実サーバが応答してしまい、LB を素通りして特定の 1 台に固定される（分散しているように見えて分散していない状態になる）
- **デフォルトルートはルータ+スイッチ側** — DSR の戻りパケットは LB を通らず実サーバから直接クライアントへ返るため

## 通信経路

### ブラウザ → frontend

```
クライアント     1.2.3.4:60000  → 203.0.113.5:3000   (VIP-A)
LB-A             宛先MACのみ書換 → node-02:3000       (宛先IPは VIP のまま)
実サーバ → クライアント（LB を経由せず直接返す = DSR）
```

### ブラウザ → api

frontend は API を中継しない。`app/backend/docs/design.md` の通り、ブラウザが `/api/config` で受け取った `API_URL` を使ってバックエンドを直接呼び出す。そのため api 側の LB-B もグローバルに置いている。

```
クライアント     1.2.3.4:60001  → 203.0.113.7:8080   (VIP-B)
LB-B             宛先MACのみ書換 → node-04:8080
実サーバ → クライアント（DSR）
```

frontend と api でオリジンが異なるため、api 側に `ALLOWED_ORIGIN`（= `app_url`）を設定している。

### 内部 → 外部（`docker pull` / `apt`）

node-02〜05 は自前のグローバルIPとデフォルトルートを持つので、NAT を経由しない。edge は共有セグメント側から直接出る。

### アプリ → DB

`192.168.1.30:3306`。app スイッチ内なので直通。DB 側は `source_ranges = [192.168.1.0/24]` でセグメント内に限定している。

## ロードバランサ

| 項目 | LB-A (frontend) | LB-B (api) |
|---|---|---|
| Terraform | `sakura_dsr_lb.frontend` | `sakura_dsr_lb.api` |
| 方式 | DSR (Direct Server Return) | 同左 |
| VIP | `ip_addresses[1]`:3000 | `ip_addresses[3]`:8080 |
| 実サーバ | node-02 / node-03 | node-04 / node-05 |
| ヘルスチェック | **TCP 接続確認のみ** | 同左 |
| 監視間隔 | 10 秒（`delay_loop`） | 同左 |
| タイムアウト / リトライ | 5 秒 / 3 回 | 同左 |
| VRID | 1 | 2 |

どちらも非冗長構成（`ip_addresses` は 1 つ）。同一セグメントに 2 台並ぶので VRID は重複させられない。

LB アプライアンスの `network_interface` は単一のスイッチしか持てず、DSR は宛先MACだけを書き換える L2 転送なので、**実サーバは必ず LB と同一セグメントにいる必要がある**。node-02〜05 をルータ+スイッチに載せているのはこのため。

## パケットフィルタ

node-02〜05 のルータ+スイッチ側 NIC にのみ適用する。app スイッチ側は素通しなので、踏み台経由の SSH は影響を受けない。

| ルール | 内容 |
|---|---|
| 1 | サービスポート（frontend は 3000、api は 8080）を全許可 |
| 2 | ICMP 許可 |
| 3 | fragment 許可 |
| 4 | TCP 宛先ポート 32768-60999 許可（サーバ発通信の戻り） |
| 5 | UDP 宛先ポート 32768-60999 許可（DNS / NTP の戻り） |
| 6 | **全拒否** |

さくらのクラウドのパケットフィルタは**ステートレス**で、どのルールにもマッチしないパケットは**許可**される。したがって末尾の明示的な deny-all と、サーバ発通信の戻りパケットの明示的な許可（ルール 4・5）が両方必要になる。

DSR では宛先が VIP のまま届き送信元はクライアントそのものなので、サービスポートを送信元 CIDR で絞ることはできない。**SSH (22) はインターネットから閉じている。**

## データベース

| 項目 | 値 |
|---|---|
| 種別 | MariaDB 10.11 / プラン 10g |
| アドレス | 192.168.1.30:3306 |
| ユーザ / DB 名 | `sakuravel_app` |
| 接続元制限 | 192.168.1.0/24 |
| バックアップ | 毎日 03:00（全曜日） |
| モニタリングスイート | 有効 |
| 冗長化 | なし（レプリカ未設定） |

## アプリケーション層

`infra/ansible/templates/compose.yml.j2` がノードの役割を見てサービスを出し分ける。

| コンテナ | ノード | ポート | 主な環境変数 |
|---|---|---|---|
| frontend | node-02 / 03 | 3000 | `API_URL` = `http://<VIP-B>:8080` |
| api | node-04 / 05 | 8080 | `DATABASE_URL`, `ALLOWED_ORIGIN` = `http://<VIP-A>:3000`, `COOKIE_SECURE=false` |

アドレス・ポート・役割は Terraform の出力を単一の出典とし、`deploy.sh` → `bootstrap.yml` が controller 上に `group_vars/app.yml` を自動生成する。`group_vars/all.yml` には秘密情報だけを置く。

## SSH

node-02〜05 はグローバルIPを持つが、パケットフィルタで 22 番を閉じているため、SSH は edge を踏み台にする。

```bash
ssh -i ~/.ssh/intern28 ubuntu@$(terraform output -raw bastion_public_ip)           # edge
ssh -i ~/.ssh/intern28 -J ubuntu@<edge> ubuntu@192.168.1.12                        # node-02
```

`-i` は ProxyJump 先の踏み台には引き継がれないため、`ssh-add ~/.ssh/intern28` するか `~/.ssh/config` に `IdentityFile` を書いておくとよい。書かないと踏み台のパスワードを聞かれる。

`terraform output ssh_commands` で全ノード分のコマンドが出る。

## 現状の制約・未対応

1. **ログインが通らない。** frontend (`VIP-A:3000`) と api (`VIP-B:8080`) は別アドレスなので、ブラウザからは**別サイト**として扱われる。セッションCookie（`app/backend/internal/handler/auth.go`）が送信されないため、認証を伴う操作が成立しない。ドメインを取得して `app.example.jp` → VIP-A / `api.example.jp` → VIP-B の A レコードを当てれば、SameSite は登録可能ドメイン単位で判定するので same-site となり、**HTTPS 無しでも直る**（併せて `ALLOWED_ORIGIN` をホスト名に更新する）
2. **ヘルスチェックが TCP のみ。** docker が LISTEN してさえいれば健全と判定されるため、DB に繋がらず 500 を返すノードにも振り分けが続く。`GET /healthz` は実装済みだが中身がスタブで常に 200 を返すため、HTTP チェックに切り替えても判定能力は変わらない。DB 疎通確認を入れてから `network.tf` を `protocol = "http"` に変更する
3. **ロードバランサが 2 台とも単一障害点。** それぞれ非冗長構成。冗長化するには `ip_addresses` を 2 つにして作り直す
4. **サービスポートが全世界に開いている。** DSR の性質上、送信元では絞れない。LB を迂回して個別ノードを直接叩けるため、偏りを避けたい場合はアプリ側の対応が要る
5. **HTTPS 未対応。** DSR LB は L4 なので TLS 終端できない。各ノードでの終端か、エンハンスドLB の前置が必要になる
6. **cloud-init は初回起動時にしか走らない。** ネットワーク設定や DSR 周りを変更しても既存ノードには反映されないため、`infra/ansible/site.yml` 側にも同じ設定を持たせている。両方を更新すること
7. **ディスクは 5 本が上限。** 作り直しを伴う変更は本数制限に当たりやすい。`-replace` を使う際は先に解放されるのを待つ必要がある
8. **OS 内部のホスト名が Terraform 上の名前と一致しない場合がある。** cloud-init が hostname を適用するのは初回起動時のみのため、改名しても既存ノードには反映されない
