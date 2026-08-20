########################################
# パケットフィルタ (ルータ+スイッチ側NIC)
########################################
# node-02〜05 はグローバルIPを持つので、サービスポート以外を閉じる。
# 特に SSH (22) はインターネットから開けない。02〜05 は app スイッチにも
# 繋がったままなので、踏み台経由 (ssh -J ubuntu@<edge> ubuntu@192.168.1.12)
# で従来通り入れる。
#
# 重要: さくらのクラウドのパケットフィルタは【ステートレス】で、
# どのルールにもマッチしないパケットは【許可】される。したがって
#   a) 末尾に明示的な deny-all を置かないとフィルタとして機能しない
#   b) サーバ発の通信の戻りパケットを明示的に許可しないと、
#      docker pull も apt も通らなくなる
# 戻りパケットは宛先ポートが Linux のエフェメラルポート範囲
# (net.ipv4.ip_local_port_range の既定 32768-60999) に入るので、そこを許可する。
# ここには何も LISTEN していないため、開けても接続は成立しない。
#
# DSR では宛先が VIP のまま届き送信元はクライアントそのものなので、
# サービスポートを送信元CIDRで絞ることはできない。LB のヘルスチェックも
# 各ノードのグローバルIPの同ポートに来るため、この許可に含まれる。

locals {
  # ロール名 → 外部に開けるサービスポート
  packet_filter_ports = {
    frontend = var.frontend_port
    api      = var.dsr_lb_port
  }

  # ノードのインデックス → 適用するパケットフィルタのロール名。
  # ルータ+スイッチ に載らない edge は null。
  node_packet_filter_role = [
    for i in range(var.node_count) :
    contains(local.frontend_node_indexes, i) ? "frontend" : (
      contains(local.api_node_indexes, i) ? "api" : null
    )
  ]

  # Linux のエフェメラルポート範囲。サーバ発通信の戻りパケットが着地する。
  ephemeral_port_range = "32768-60999"
}

resource "sakura_packet_filter" "node" {
  for_each = local.packet_filter_ports

  name        = "pf-${each.key}"
  description = "intern2026 ${each.key} nodes (public NIC)"
  zone        = var.zone
}

resource "sakura_packet_filter_rules" "node" {
  for_each = local.packet_filter_ports

  packet_filter_id = sakura_packet_filter.node[each.key].id
  zone             = var.zone

  # 上から順に評価される。source_network を null にすると送信元は any。
  expression = [
    {
      protocol         = "tcp"
      source_network   = null
      source_port      = null
      destination_port = tostring(each.value)
      allow            = true
      description      = "service port (via LB VIP and health check)"
    },
    {
      protocol         = "tcp"
      source_network   = null
      source_port      = null
      destination_port = "80"
      allow            = true
      description      = "HTTP redirect and ACME HTTP-01 challenge"
    },
    {
      protocol         = "tcp"
      source_network   = null
      source_port      = null
      destination_port = "443"
      allow            = true
      description      = "HTTPS"
    },
    {
      protocol         = "icmp"
      source_network   = null
      source_port      = null
      destination_port = null
      allow            = true
      description      = "ping for diagnostics"
    },
    {
      protocol         = "fragment"
      source_network   = null
      source_port      = null
      destination_port = null
      allow            = true
      description      = "fragmented packets"
    },
    {
      protocol         = "tcp"
      source_network   = null
      source_port      = null
      destination_port = local.ephemeral_port_range
      allow            = true
      description      = "return packets for server-initiated TCP (docker pull, apt)"
    },
    {
      protocol         = "udp"
      source_network   = null
      source_port      = null
      destination_port = local.ephemeral_port_range
      allow            = true
      description      = "return packets for server-initiated UDP (DNS, NTP)"
    },
    {
      protocol         = "ip"
      source_network   = null
      source_port      = null
      destination_port = null
      allow            = false
      description      = "deny all (stateless filter: this must be last)"
    },
  ]
}
