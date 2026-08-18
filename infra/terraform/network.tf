########################################
# スイッチ (vSwitch)
########################################
# ゾーン内のリソース数上限により作成できるのは1本。
# 全ノードとデータベースアプライアンスがこの1本に接続する。

resource "sakura_vswitch" "app" {
  name        = "intern2026-app-sw"
  description = "intern2026 app segment (${var.app_net_cidr})"
  zone        = var.zone
}

locals {
  app_net_cidr = var.app_net_cidr

  # app セグメントのプレフィックス長 (netplan の addresses に使う)
  app_prefix_length = split("/", local.app_net_cidr)[1]

  # node-01 → 192.168.1.11, node-02 → 192.168.1.12, …
  node_private_ips = [
    for i in range(var.node_count) : cidrhost(local.app_net_cidr, var.node_ip_offset + i)
  ]

  # node-01 が NAT ゲートウェイを兼ねる。プライベート専用ノードと
  # データベースアプライアンスはこのアドレスをデフォルトゲートウェイにする。
  gateway_private_ip = local.node_private_ips[0]

  db_private_ip = cidrhost(local.app_net_cidr, var.db_ip_offset)
}
