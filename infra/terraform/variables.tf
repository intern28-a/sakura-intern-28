########################################
# 認証・ゾーン
########################################

variable "sakura_access_token" {
  description = "さくらのクラウド API アクセストークン。環境変数 SAKURA_ACCESS_TOKEN でも供給可。"
  type        = string
  default     = null
  sensitive   = true
}

variable "sakura_access_token_secret" {
  description = "さくらのクラウド API アクセストークンシークレット。環境変数 SAKURA_ACCESS_TOKEN_SECRET でも供給可。"
  type        = string
  default     = null
  sensitive   = true
}

variable "zone" {
  description = "リソースを作成するゾーン。tk1a / tk1b / is1a / is1b / is1c から選択。"
  type        = string
  default     = "is1b"
}

########################################
# ネットワーク
########################################
# セグメントは2本。
#   app スイッチ      : 全ノード + データベースアプライアンスのプライベート網
#   ルータ+スイッチ   : グローバル網。LB アプライアンス2台と node-02〜05 が載る
# ゾーン内のスイッチ上限は3本なので、この2本で収まる。

variable "app_net_cidr" {
  description = "全ノードとデータベースアプライアンスが接続する app セグメントの CIDR。"
  type        = string
  default     = "192.168.1.0/24"
}

variable "node_ip_offset" {
  description = "app セグメント内でノードに割り当てるホスト番号の開始値。先頭ノード (edge) が .11 になる。"
  type        = number
  default     = 11
}

variable "db_ip_offset" {
  description = "app セグメント内でデータベースアプライアンスに割り当てるホスト番号。"
  type        = number
  default     = 30
}

variable "pub_netmask" {
  description = <<-EOT
    ルータ+スイッチ に払い出すグローバルセグメントのプレフィックス長。26 / 27 / 28 のいずれか。
    28 なら 16 アドレスのうち5個がネットワーク・ルータ・ブロードキャスト用に予約され、
    残り 11 個が払い出される。
    冗長構成 (dsr_lb_redundant = true) では
    LB本体×4 + VIP×2 + ノード×4 = 10 個使うので 28 でぎりぎり収まる。
    ノードを増やす場合は 27 に広げること。
  EOT
  type        = number
  default     = 28

  validation {
    condition     = contains([26, 27, 28], var.pub_netmask)
    error_message = "pub_netmask は 26 / 27 / 28 のいずれかを指定してください。"
  }
}

variable "pub_band_width" {
  description = "ルータ+スイッチ の帯域 (Mbps)。100 / 250 / 500 / 1000 などから選択。"
  type        = number
  default     = 100
}

########################################
# サーバー
########################################
# 5台とも同一スペックの汎用ノード。
#   edge (先頭)   : 共有セグメント + app スイッチ。Ansible コントローラ兼踏み台
#   node-02〜05   : app スイッチ + ルータ+スイッチ。自前のグローバルIPで外部へ出る

variable "node_count" {
  description = "作成するサーバーの台数。"
  type        = number
  default     = 5
}

variable "node_name_prefix" {
  description = "2台目以降のサーバー名の接頭辞。node-02 … node-05 のように連番が付く。"
  type        = string
  default     = "node"
}

variable "edge_node_name" {
  description = <<-EOT
    先頭ノードの名前。この1台だけがグローバルIPを持ち、踏み台・NAT ゲートウェイ・
    外部から VIP への DNAT 入口を兼ねるため、他の汎用ノードとは別の名前を付ける。
  EOT
  type        = string
  default     = "edge"
}

variable "server_core" {
  description = "サーバー1台あたりの仮想コア数。"
  type        = number
  default     = 4
}

variable "server_memory" {
  description = "サーバー1台あたりのメモリ (GiB)。"
  type        = number
  default     = 12
}

variable "disk_plan" {
  description = "ディスクのプラン。ssd / hdd のいずれか。標準プランは hdd。"
  type        = string
  default     = "hdd"

  validation {
    condition     = contains(["ssd", "hdd"], var.disk_plan)
    error_message = "disk_plan は ssd か hdd のいずれかを指定してください。"
  }
}

variable "disk_size" {
  description = <<-EOT
    ディスクのサイズ (GiB)。標準プランで指定できるのは
    40 / 60 / 80 / 100 / 250 / 500 / 750 / 1024 / 2048 ... のいずれか。
    プロジェクト全体のディスク合計サイズにも上限があり、超えると
    作成時に limit_size_in_account エラーになる。
  EOT
  type        = number
  default     = 100

  validation {
    condition     = contains([20, 40, 60, 80, 100, 250, 500, 750, 1024, 2048, 4096], var.disk_size)
    error_message = "disk_size にはプランで利用可能なサイズを指定してください。"
  }
}

variable "server_password" {
  description = "サーバーの初期パスワード (SSH 公開鍵認証を使用する場合でも必要)"
  type        = string
  sensitive   = true
}

variable "server_ssh_public_key_path" {
  description = "サーバーへの SSH 公開鍵認証で使用する公開鍵ファイルのパス"
  type        = string
  default     = ""
}

########################################
# データベースアプライアンス
########################################

variable "db_username" {
  description = "データベースのユーザー名。同名のデータベースが作成される。"
  type        = string
  default     = "sakuravel_app"
}

variable "db_password" {
  description = "データベースのパスワード"
  type        = string
  sensitive   = true
}

variable "db_plan" {
  description = "データベースアプライアンスのプラン。10g / 30g / 90g / 240g / 500g / 1t から選択。"
  type        = string
  default     = "10g"
}

variable "db_parameters" {
  description = <<-EOT
    データベースアプライアンスに設定する RDBMS 固有パラメータ。
    チューニング演習でここに max_connections や innodb_buffer_pool_size などを
    追記するとコード管理できる。設定可能なキーは
    `usacloud database list-parameters <ID>` で確認する。
  EOT
  type        = map(string)
  default     = {}
}

########################################
# DSR ロードバランサ (2台)
########################################
# ルータ+スイッチ 上に2台並べ、役割ごとに振り分けを分ける。
#   LB-A: VIP-A:frontend_port → node-02 / node-03 (frontend)
#   LB-B: VIP-B:dsr_lb_port   → node-04 / node-05 (api)
# 本体アドレスと VIP はどちらも sakura_internet.pub の払い出しから取るため、
# オフセット変数は持たない (network.tf の locals を参照)。
# 既定では両方とも冗長構成 (dsr_lb_redundant = true)。

variable "dsr_lb_plan" {
  description = "DSR ロードバランサのプラン。standard / highspec のいずれか。2台とも同じプランを使う。"
  type        = string
  default     = "standard"

  validation {
    condition     = contains(["standard", "highspec"], var.dsr_lb_plan)
    error_message = "dsr_lb_plan は standard か highspec のいずれかを指定してください。"
  }
}

variable "dsr_lb_redundant" {
  description = <<-EOT
    ロードバランサを冗長構成にするか。
    true にすると LB 1台あたりアプライアンス実機が2台作られ、VRRP でアクティブ/
    スタンバイを組む。VIP はアクティブ側が保持し、片系障害時に引き継がれる。
    そのぶんグローバルIPを LB あたり2個 (本体) 消費する。
    false は実機1台の非冗長構成で、消費は LB あたり1個。
    切り替えは LB の作り直しになり、アドレスの割り当て順がずれるため VIP も変わる。
  EOT
  type        = bool
  default     = true
}

variable "dsr_lb_a_vrid" {
  description = <<-EOT
    frontend 用ロードバランサ (LB-A) の VRID。
    LB-B と同一セグメントに並ぶので、dsr_lb_b_vrid と重複させないこと。
  EOT
  type        = number
  default     = 1
}

variable "dsr_lb_b_vrid" {
  description = "api 用ロードバランサ (LB-B) の VRID。dsr_lb_a_vrid と重複させないこと。"
  type        = number
  default     = 2
}

variable "frontend_port" {
  description = <<-EOT
    LB-A の VIP と frontend コンテナが待ち受けるポート番号。
    DSR 方式ではポート変換が行われないため、VIP と実サーバで同じ値になる。
  EOT
  type        = number
  default     = 3000
}

variable "dsr_lb_port" {
  description = <<-EOT
    LB-B の VIP と api コンテナが待ち受けるポート番号。
    DSR 方式ではポート変換が行われないため、VIP と実サーバで同じ値になる。
  EOT
  type        = number
  default     = 8080
}
