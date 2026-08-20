########################################
# 接続情報
########################################

output "app_url" {
  description = "ブラウザからアクセスする frontend の URL。LB-A の VIP。"
  value       = "https://${local.lb_a_vip}"
}

output "api_url" {
  description = <<-EOT
    frontend の API_URL に渡す相対URL。アクセス元のIPまたはドメインに関係なく
    同一オリジンの /api/ を使い、LB-Bへ中継する。
  EOT
  value       = "/api"
}

output "frontend_ip" {
  description = "frontend HTTPS 用のグローバル VIP。"
  value       = local.lb_a_vip
}

output "api_ip" {
  description = "API HTTPS 用のグローバル VIP。"
  value       = local.lb_b_vip
}

output "bastion_public_ip" {
  description = <<-EOT
    edge のグローバルIP (共有セグメント)。
    Ansible コントローラ兼踏み台。アプリのトラフィックは通らない。
  EOT
  value       = sakura_server.node[0].ip_address
}

output "ssh_commands" {
  description = <<-EOT
    各ノードへの SSH コマンド。node-02〜05 はグローバルIPを持つが、
    パケットフィルタで 22 番を閉じてあるので edge を踏み台にする。
  EOT
  value = {
    for i, s in sakura_server.node : s.name => (
      i == 0
      ? "ssh ubuntu@${sakura_server.node[0].ip_address}"
      : "ssh -J ubuntu@${sakura_server.node[0].ip_address} ubuntu@${local.node_private_ips[i]}"
    )
  }
}

########################################
# ネットワーク
########################################

output "node_private_ips" {
  description = "各ノードの app セグメント上のプライベートIP。"
  value = {
    for i, s in sakura_server.node : s.name => local.node_private_ips[i]
  }
}

output "node_public_ips" {
  description = "ルータ+スイッチ に載るノード (node-02〜05) のグローバルIP。"
  value = {
    for i in local.app_node_indexes : local.node_names[i] => local.node_public_ips[i]
  }
}

output "node_roles" {
  description = <<-EOT
    ルータ+スイッチ に載るノードの役割。frontend / api のいずれか。
    Ansible のインベントリのグループ分けと compose の出し分けに使う。
  EOT
  value = {
    for i in local.app_node_indexes :
    local.node_names[i] => contains(local.frontend_node_indexes, i) ? "frontend" : "api"
  }
}

output "dns_servers" {
  description = "ゾーンの DNS サーバ。ノードの netplan に入れる。"
  value       = data.sakura_zone.current.dns_servers
}

output "pub_network" {
  description = "ルータ+スイッチ の払い出し内容。アドレス割り当ての確認用。"
  value = {
    network_address = sakura_internet.pub.network_address
    netmask         = sakura_internet.pub.netmask
    gateway         = sakura_internet.pub.gateway
    ip_addresses    = sakura_internet.pub.ip_addresses
  }
}

output "app_net_cidr" {
  description = "app セグメントの CIDR。Ansible が netplan のプレフィックス長を取り出すのに使う。"
  value       = local.app_net_cidr
}

output "app_switch_id" {
  description = "全ノードとデータベースアプライアンスが接続する app セグメントの vSwitch ID。"
  value       = sakura_vswitch.app.id
}

output "db_host" {
  description = "データベースアプライアンスの接続先アドレス。"
  value       = local.db_private_ip
}

########################################
# ロードバランサ
########################################

output "lb_frontend" {
  description = "frontend 用ロードバランサ (LB-A) の構成。"
  value = {
    appliance_ip = local.lb_a_public_ip
    vip          = local.lb_a_vip
    port         = var.frontend_port
    real_servers = [for i in local.frontend_node_indexes : local.node_public_ips[i]]
  }
}

output "lb_api" {
  description = "api 用ロードバランサ (LB-B) の構成。"
  value = {
    appliance_ip = local.lb_b_public_ip
    vip          = local.lb_b_vip
    port         = var.dsr_lb_port
    real_servers = [for i in local.api_node_indexes : local.node_public_ips[i]]
  }
}
