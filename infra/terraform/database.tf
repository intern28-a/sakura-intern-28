########################################
# データベースアプライアンス: intern2026-db
########################################
# 冗長化・レプリケーションなし → replica_* は指定しない。
# database_version は MariaDB = "10.11" (アプリの docker-compose と揃える)。
#
# NOTE: app セグメントにはルータが無い。DB は source_ranges でセグメント内に
#       限定しており外部と通信しないため gateway は実質使われないが、
#       network_interface の必須項目なので edge のプライベートIPを入れておく。

resource "sakura_database" "db" {
  name = "db"
  zone = var.zone

  database_type    = "mariadb"
  database_version = "10.11"
  plan             = var.db_plan

  username            = var.db_username
  password_wo         = var.db_password
  password_wo_version = 1

  network_interface = {
    vswitch_id    = sakura_vswitch.app.id
    ip_address    = local.db_private_ip
    netmask       = tonumber(local.app_prefix_length)
    gateway       = local.gateway_private_ip
    port          = 3306
    source_ranges = [local.app_net_cidr]
  }

  # RDBMS 固有パラメータ。チューニング演習では var.db_parameters に
  # max_connections / innodb_buffer_pool_size などを足してコード管理する。
  parameters = var.db_parameters

  # 定期バックアップ: 毎日 03:00
  backup = {
    days_of_week = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"]
    time         = "03:00"
  }

  # モニタリングスイート: 連携する
  monitoring_suite = {
    enabled = true
  }
}
