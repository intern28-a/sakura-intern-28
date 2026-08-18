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
  default     = "tk1a"
}

########################################
# ネットワーク
########################################
# ゾーン内のリソース数上限により vSwitch は1本のみ作成する。
# 全ノードとデータベースアプライアンスがこの1本を共有する。

variable "app_net_cidr" {
  description = "全ノードとデータベースアプライアンスが接続する app セグメントの CIDR。"
  type        = string
  default     = "192.168.1.0/24"
}

variable "node_ip_offset" {
  description = "app セグメント内でノードに割り当てるホスト番号の開始値。node-01 が .11 になる。"
  type        = number
  default     = 11
}

variable "db_ip_offset" {
  description = "app セグメント内でデータベースアプライアンスに割り当てるホスト番号。"
  type        = number
  default     = 30
}

########################################
# サーバー
########################################
# 5台とも同一スペックの汎用ノード。役割 (LB / App / DB) は構築後に決める。
# グローバルIP (共有セグメント) を持つのは node-01 のみで、
# node-02〜05 は node-01 の NAT 経由で外部へ出る。

variable "node_count" {
  description = "作成するサーバーの台数。"
  type        = number
  default     = 5
}

variable "node_name_prefix" {
  description = "サーバー名の接頭辞。node-01 … node-05 のように連番が付く。"
  type        = string
  default     = "node"
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
