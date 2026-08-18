########################################
# スイッチ (vSwitch)
########################################
# ゾーン内のリソース数上限により作成できるのは1本。
# 全ノードとデータベースアプライアンスがこの1本に接続する。

resource "sakura_vswitch" "app" {
  name        = "app-sw"
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

########################################
# DSR ロードバランサ
########################################
# app スイッチ上に VIP を持つ L4 ロードバランサアプライアンス。
# 実サーバは node-02〜05 で、node-01 は入口 + NAT ゲートウェイなので含めない。
#
# DSR (Direct Server Return) 方式なので、
#   - 転送時に書き換わるのは宛先 MAC だけで、宛先IP は VIP のまま実サーバに届く
#   - 戻りパケットは LB を通らず実サーバから直接クライアントへ返る
# この2点から、実サーバ側に以下の設定が必要になる。これらは Ansible 側で行う。
#   a) VIP (local.dsr_lb_vip) を lo に /32 で割り当てる
#      → Docker の PREROUTING は `-m addrtype --dst-type LOCAL -j DOCKER` で
#        条件付けされているため、VIP がローカル扱いにならないと公開ポート 8080 の
#        DNAT が発火せず、パケットは黙って捨てられる。
#   b) net.ipv4.conf.all.arp_ignore=1 / arp_announce=2 を設定する
#      → lo に付けた VIP でも Linux は既定で ethernet 側から ARP 応答してしまう。
#        抑止しないと VIP の ARP に実サーバが答え、LB を素通りして特定の1台に
#        固定される (分散しているように見えて分散していない状態になる)。

locals {
  dsr_lb_private_ip = cidrhost(local.app_net_cidr, var.dsr_lb_ip_offset)
  dsr_lb_vip        = cidrhost(local.app_net_cidr, var.dsr_lb_vip_offset)

  # node-02 以降を実サーバにする (node-01 は除外)
  dsr_lb_real_server_ips = slice(local.node_private_ips, 1, var.node_count)
}

resource "sakura_dsr_lb" "app" {
  name        = "lb"
  description = "intern2026 app DSR load balancer"
  tags        = ["intern2026"]
  zone        = var.zone

  plan = var.dsr_lb_plan

  # app スイッチにはルータが無いため、データベースアプライアンスと同じく
  # NAT ゲートウェイを兼ねる node-01 をデフォルトゲートウェイにする。
  # ip_addresses は非冗長構成なので1つだけ (冗長化する場合は2つ指定する)。
  network_interface = {
    vswitch_id   = sakura_vswitch.app.id
    vrid         = var.dsr_lb_vrid
    ip_addresses = [local.dsr_lb_private_ip]
    netmask      = tonumber(local.app_prefix_length)
    gateway      = local.gateway_private_ip
  }

  vip = [{
    vip         = local.dsr_lb_vip
    port        = var.dsr_lb_port
    delay_loop  = 10
    description = "api"

    # TODO: ヘルスチェックを HTTP に切り替える。
    #   現状は TCP 接続確認のみ。api は GET / を持たず (cmd/api/main.go の routes)
    #   404 を返すため、いま HTTP チェックにすると全ノードが不健全と判定される。
    #   また TCP チェックでは docker が 8080 を LISTEN してさえいれば健全と見なされ、
    #   DB に繋がらず全リクエストが 500 になっているノードにも振り続けてしまう。
    #   cmd/api/main.go の GET /healthz スタブに DB 疎通確認を実装したうえで、
    #   下記を protocol = "http" / path = "/healthz" / status = 200 に変更する。
    server = [
      for ip in local.dsr_lb_real_server_ips : {
        ip_address      = ip
        protocol        = "tcp"
        connect_timeout = 5
        retry           = 3
        enabled         = true
      }
    ]
  }]
}
