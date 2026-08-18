# 28卒エンジニアインターン用インフラ

さくらのクラウド上に、汎用ノード5台構成を Terraform で構築します。

## 構成

```
                        [Internet]
                             │
                             ▼ 共有セグメント（グローバルIP）
  ┌───────────────────────────────────────────────────┐
  │  node-01   192.168.1.11   踏み台 + NAT ゲートウェイ  │
  └───────────────────────────────────────────────────┘
        │  intern2026-app-sw  192.168.1.0/24
        ├── node-02  192.168.1.12  ┐
        ├── node-03  192.168.1.13  │ プライベートのみ
        ├── node-04  192.168.1.14  │ default route → .11
        ├── node-05  192.168.1.15  ┘
        └── intern2026-db  192.168.1.30 （マネージドDBアプライアンス / MariaDB 10.11）
```

vSwitch はゾーン内のリソース数上限により **1本のみ**です。全ノードとDBがこの1本を共有します。

- 5台とも同一スペック（仮想4コア / 12GB メモリ / 標準プラン 100GiB ディスク）の汎用ノードです。
  役割（LB / App / DB など）は構築後に決めます。
- **グローバルIPを持つのは node-01 だけ**です。node-02〜05 は node-01 の NAT（iptables MASQUERADE）
  経由で外部と通信します。
- cloud-init で全ノードに Docker Engine + Compose V2 が入ります。node-02〜05 は node-01 の NAT が
  立ち上がるまで待ってから Docker のインストールに進みます（最大10分リトライ）。

## 構築手順

構築前に tfvars をコピーして編集します：

```
cp secret.auto.tfvars.example secret.auto.tfvars
```

クレデンシャルなどは1Passwordに入ってます。

## Ansible デプロイ

手動で準備するシークレット、SSH鍵、Terraform実行、Ansibleデプロイの詳細は
[`docs/dev/DEPLOYMENT.md`](docs/dev/DEPLOYMENT.md) を参照してください。

VM 作成後、ローカルの `group_vars/all.yml` に DB・レジストリ認証情報を設定します。

```bash
cd ../ansible
./deploy.sh
```

`deploy.sh` が Terraform の output から node-01 の公開 IP を取得し、SSH 秘密鍵・`all.yml` の確認、bootstrap、node-01 から node-02〜05 へのデプロイまでを自動実行します。node-01 上でのファイル編集や inventory の IP 編集は不要です。

```bash
cd terraform
terraform init
terraform plan     # サーバ5・ディスク5・スイッチ1・DB1 = 12リソースになることを確認
terraform apply
```

データベースアプライアンスの作成に10〜20分かかるため、`apply` 全体は長めです。

## 各ノードへの接続

```bash
terraform output ssh_commands
```

node-01 には直接、node-02〜05 には node-01 を踏み台（ProxyJump）にして入ります。

```bash
ssh ubuntu@$(terraform output -raw bastion_public_ip)          # node-01
ssh -J ubuntu@$(terraform output -raw bastion_public_ip) ubuntu@192.168.1.12   # node-02
```

### 公開鍵を配布する

`secret.auto.tfvars` の `server_ssh_public_key_path` が空のまま `apply` すると、
サーバーに公開鍵が一切インストールされず**パスワード認証だけ**になります
（`Permission denied (publickey,password)` で鍵が弾かれる場合はこれ）。

> **注意**: `server_ssh_public_key_path` は cloud-init 経由で反映されるため、
> **サーバー作成時にしか効きません**。構築後に値を変えて `apply` すると
> terraform は "update in-place" と表示しますが、cloud-init は初回起動時にしか
> 走らないので稼働中のサーバーに鍵は入りません。以下で手動配布してください。

VMのパスワード（1Password）を使って全ノードに鍵を配ります。

```bash
cd terraform
BASTION=$(terraform output -raw bastion_public_ip)

# node-01（直接）
ssh-copy-id ubuntu@$BASTION

# node-02〜05（node-01 を踏み台に）
for i in 2 3 4 5; do
  ssh-copy-id -o ProxyJump=ubuntu@$BASTION ubuntu@192.168.1.1$i
done
```

各コマンドでパスワードを聞かれます。node-01 に鍵が入った後は踏み台側の入力は不要になります。

`~/.ssh/config` に書いておくと楽です：

```
Host node-01
  HostName <bastion_public_ip>
  User ubuntu

Host node-02 node-03 node-04 node-05
  User ubuntu
  ProxyJump node-01

Host node-02
  HostName 192.168.1.12
# node-03 → .13, node-04 → .14, node-05 → .15
```

## 主な変数

`terraform/variables.tf` を参照。よく触るのは以下です。

| 変数 | デフォルト | 用途 |
|---|---|---|
| `node_count` | `5` | 台数 |
| `server_core` / `server_memory` | `4` / `12` | ノードのスペック |
| `disk_plan` / `disk_size` | `"hdd"` / `100` | 標準プラン 100GiB |
| `app_net_cidr` | `"192.168.1.0/24"` | セグメントの CIDR |
| `db_plan` | `"10g"` | DBアプライアンスのプラン |
| `db_parameters` | `{}` | DBアプライアンスのパラメータ（チューニング用） |

## トラブルシュート

cloud-init の進行状況は各ノードで確認します。

```bash
cloud-init status --wait
sudo cat /var/log/cloud-init-output.log
```

- node-02〜05 で `ip route show default` が空 → netplan の生成に失敗しています。
  `sudo cat /etc/netplan/60-private-nic.yaml` を確認してください。
- node-02〜05 から外に出られない → node-01 側で `sysctl net.ipv4.ip_forward` と
  `sudo iptables -t nat -L POSTROUTING -n` を確認してください。

### リソース上限エラー

さくらのクラウド側にプロジェクト単位・ゾーン単位の上限があり、超えると `apply` が 409 で失敗します。

| ErrorCode | 意味 | 対処 |
|---|---|---|
| `limit_count_in_zone` | ゾーン内のリソース**数**上限 | スイッチ等の本数を減らす（現構成は vSwitch 1本） |
| `limit_size_in_account` | プロジェクトの合計**サイズ**上限 | `disk_size` を下げる。既存の未使用ディスクを消して枠を空けるのも有効 |
