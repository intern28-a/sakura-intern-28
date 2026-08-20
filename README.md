# さくらインターネット　28年度エンジニアインターン

Twitter/X ライクな SNS アプリ **Sakuravel** を、さくらのクラウド上に構築したインフラへデプロイし、
パフォーマンスチューニングを行うためのリポジトリです。

## 概要

| レイヤ   | 内容                                                                                                    |
| -------- | ------------------------------------------------------------------------------------------------------- |
| アプリ   | Go 1.25 + MariaDB の REST API（チューニング対象）と、配布イメージの Next.js フロントエンド              |
| インフラ | さくらのクラウド `tk1a` ゾーンに Terraform で汎用ノード5台 + マネージドDB + DSR ロードバランサ2台を構築 |
| デプロイ | edge ノードを Ansible コントローラ兼踏み台にして、node-02〜05 へ Docker Compose で配布                  |

構成は edge（踏み台/Ansible）、node-02・03（frontend）、node-04・05（api）、マネージド MariaDB。
frontend / api それぞれに DSR ロードバランサの VIP を持たせ、HTTPS で公開しています。
ロードバランサは 2 台とも冗長構成（アプライアンス実機 2 台の VRRP）です。
詳細な構成図・IP 割り当て・既知の制約は [`infra/ARCHITECTURE.md`](infra/ARCHITECTURE.md) を参照してください。

## ディレクトリ構成

```
.
├── app/     アプリケーション
│   └── backend/    Go + MariaDB の REST API（チューニング対象）+ 仕様ドキュメント
└── infra/   インフラ
    ├── terraform/  サーバ・ネットワーク・DB・LB の定義
    └── ansible/    デプロイ（deploy.sh 一発で実行）
```

## ドキュメント

| ドキュメント                                       | 内容                                                               |
| -------------------------------------------------- | ------------------------------------------------------------------ |
| **[デプロイ手順書](infra/docs/dev/DEPLOYMENT.md)** | **本番環境への構築・デプロイ手順（Terraform → Ansible）**          |
| [infra/ARCHITECTURE.md](infra/ARCHITECTURE.md)     | インフラ構成の詳細（構成図・IP・LB・パケットフィルタ・既知の制約） |
| [infra/README.md](infra/README.md)                 | Terraform の変数・SSH 接続・トラブルシュート                       |
| [app/README.md](app/README.md)                     | アプリのローカル起動、環境変数、API 一覧                           |
| [app/backend/docs/](app/backend/docs/)             | API 仕様・DB 設計・要件定義・ダミーデータ投入手順                  |

## クイックスタート

### ローカルで動かす

```bash
cd app/backend
docker compose up -d      # frontend :3000 / api :8080 / MariaDB :3306
```

事前に `docker-compose.yml` の `frontend` イメージ名の置き換えが必要です。詳細は [`app/README.md`](app/README.md) を参照。

### クラウドへデプロイする

```bash
cd infra/terraform
terraform init && terraform apply    # VM・DB・LB を作成

cd ../ansible
./deploy.sh                          # node-02〜05 へアプリを配布・起動
```

シークレット（`secret.auto.tfvars` / `group_vars/all.yml`）の準備を含む手順は
[**デプロイ手順書**](infra/docs/dev/DEPLOYMENT.md) にまとまっています。クレデンシャルは 1Password を参照。

デプロイ後のアクセス先は Terraform の出力で確認します。

```bash
terraform output app_url        # フロントエンドの URL
terraform output ssh_commands   # 各ノードへの SSH コマンド
```
