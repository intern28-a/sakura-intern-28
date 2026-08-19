########################################
# app スイッチ (vSwitch)
########################################
# 全ノードとデータベースアプライアンスが接続するプライベート網。
# node-02〜05 はここでデータベースアプライアンス (192.168.1.30) に到達する。

resource "sakura_vswitch" "app" {
  name        = "app-sw"
  description = "intern2026 app segment (${var.app_net_cidr})"
  zone        = var.zone
}

########################################
# ルータ+スイッチ (グローバルセグメント)
########################################
# LB アプライアンス2台と node-02〜05 が載るグローバル網。
#
# LB アプライアンスをここに置く理由:
#   - LB の network_interface は単一の vswitch_id しか持てない
#   - DSR は宛先MACだけを書き換える L2 転送なので、振り分け先は LB と同一
#     セグメントにいなければならない
# この2点から、グローバルな VIP を持たせるには LB ごとこちら側へ移し、
# 実サーバ (node-02〜05) も同じセグメントに載せる必要がある。
#
# なお、さくらのルータ+スイッチは DHCP を提供しない。接続するサーバは
# グローバル側も静的にアドレスを設定する (cloud-init.yaml.tftpl を参照)。

resource "sakura_internet" "pub" {
  name        = "pub-router"
  description = "intern2026 public segment"
  tags        = ["intern2026"]
  zone        = var.zone

  netmask    = var.pub_netmask
  band_width = var.pub_band_width
}

locals {
  app_net_cidr = var.app_net_cidr

  # app セグメントのプレフィックス長 (netplan の addresses に使う)
  app_prefix_length = split("/", local.app_net_cidr)[1]

  # edge → 192.168.1.11, node-02 → 192.168.1.12, …
  node_private_ips = [
    for i in range(var.node_count) : cidrhost(local.app_net_cidr, var.node_ip_offset + i)
  ]

  # 先頭の1台だけは役割が違うので名前も分ける (Ansible コントローラ + 踏み台)。
  # インデックスは他ノードと連続させたままにして、IP の対応関係を崩さない。
  node_names = [
    for i in range(var.node_count) :
    i == 0 ? var.edge_node_name : format("%s-%02d", var.node_name_prefix, i + 1)
  ]

  # ノードの役割。インデックスは node_names / node_private_ips と対応する。
  frontend_node_indexes = [1, 2] # node-02, node-03
  api_node_indexes      = [3, 4] # node-04, node-05
  app_node_indexes      = concat(local.frontend_node_indexes, local.api_node_indexes)

  # データベースアプライアンスのデフォルトゲートウェイ。
  # DB は app セグメント内の通信しかしないので実質使われないが、
  # network_interface の必須項目なので edge のアドレスを入れておく。
  gateway_private_ip = local.node_private_ips[0]

  db_private_ip = cidrhost(local.app_net_cidr, var.db_ip_offset)

  # ルータ+スイッチ から払い出されたグローバルIPの割り当て。
  # ip_addresses は昇順の払い出し可能アドレス一覧なので、先頭から順に取る。
  #   [0] LB-A 本体 / [1] VIP-A / [2] LB-B 本体 / [3] VIP-B / [4]〜 ノード4台
  lb_a_public_ip = sakura_internet.pub.ip_addresses[0]
  lb_a_vip       = sakura_internet.pub.ip_addresses[1]
  lb_b_public_ip = sakura_internet.pub.ip_addresses[2]
  lb_b_vip       = sakura_internet.pub.ip_addresses[3]

  # ノードのグローバルIP。ルータ+スイッチ に載らない edge は null。
  # node_private_ips と同じくインデックスで引ける形にしておく。
  node_public_ips = [
    for i in range(var.node_count) :
    contains(local.app_node_indexes, i)
    ? sakura_internet.pub.ip_addresses[4 + index(local.app_node_indexes, i)]
    : null
  ]

  # 各ノードが lo に付ける VIP。役割によって向き先の LB が変わる。
  node_dsr_vip = [
    for i in range(var.node_count) :
    contains(local.frontend_node_indexes, i) ? local.lb_a_vip : (
      contains(local.api_node_indexes, i) ? local.lb_b_vip : null
    )
  ]

  # 各ノードが待ち受けるサービスポート。パケットフィルタと DSR VIP の両方で使う。
  node_service_port = [
    for i in range(var.node_count) :
    contains(local.frontend_node_indexes, i) ? var.frontend_port : (
      contains(local.api_node_indexes, i) ? var.dsr_lb_port : null
    )
  ]
}

########################################
# DSR ロードバランサ (2台)
########################################
# ルータ+スイッチ 上に2台並べ、役割ごとに振り分けを分ける。
#
#   LB-A: VIP-A:${frontend_port} → node-02 / node-03 (frontend)
#   LB-B: VIP-B:${dsr_lb_port}   → node-04 / node-05 (api)
#
# ブラウザは frontend と api の両方を直接叩く (app/backend/docs/design.md:25 の
# 「ブラウザはバックエンドを直接呼び出す（フロントは中継しない）」)。
# そのため api 側の LB-B もグローバルに置く必要がある。
#
# DSR (Direct Server Return) 方式なので、
#   - 転送時に書き換わるのは宛先 MAC だけで、宛先IP は VIP のまま実サーバに届く
#   - 戻りパケットは LB を通らず実サーバから直接クライアントへ返る
# この2点から、実サーバ側に以下の設定が必要になる。
#   a) 自分の役割の VIP を lo に /32 で割り当てる
#      → Docker の PREROUTING は `-m addrtype --dst-type LOCAL -j DOCKER` で
#        条件付けされているため、VIP がローカル扱いにならないと公開ポートの
#        DNAT が発火せず、パケットは黙って捨てられる。
#   b) net.ipv4.conf.all.arp_ignore=1 / arp_announce=2 を設定する
#      → lo に付けた VIP でも Linux は既定で ethernet 側から ARP 応答してしまう。
#        抑止しないと VIP の ARP に実サーバが答え、LB を素通りして特定の1台に
#        固定される (分散しているように見えて分散していない状態になる)。
# また戻りパケットが直接クライアントへ返る以上、実サーバのデフォルトルートは
# ルータ+スイッチ側を向いている必要がある。

resource "sakura_dsr_lb" "frontend" {
  name        = "lb-frontend"
  description = "intern2026 frontend load balancer (node-02/03)"
  tags        = ["intern2026"]
  zone        = var.zone

  plan = var.dsr_lb_plan

  # ip_addresses は非冗長構成なので1つだけ (冗長化する場合は2つ指定する)。
  # VRID は同一セグメントの LB-B と重複させないこと。
  network_interface = {
    vswitch_id   = sakura_internet.pub.vswitch_id
    vrid         = var.dsr_lb_a_vrid
    ip_addresses = [local.lb_a_public_ip]
    netmask      = sakura_internet.pub.netmask
    gateway      = sakura_internet.pub.gateway
  }

  vip = [{
    vip         = local.lb_a_vip
    port        = var.frontend_port
    delay_loop  = 10
    description = "frontend"

    # TODO: ヘルスチェックを HTTP に切り替える。
    #   現状は TCP 接続確認のみで、docker が待ち受けてさえいれば健全と見なされる。
    server = [
      for i in local.frontend_node_indexes : {
        ip_address      = local.node_public_ips[i]
        protocol        = "tcp"
        connect_timeout = 5
        retry           = 3
        enabled         = true
      }
    ]
  }]
}

resource "sakura_dsr_lb" "api" {
  name        = "lb-api"
  description = "intern2026 api load balancer (node-04/05)"
  tags        = ["intern2026"]
  zone        = var.zone

  plan = var.dsr_lb_plan

  network_interface = {
    vswitch_id   = sakura_internet.pub.vswitch_id
    vrid         = var.dsr_lb_b_vrid
    ip_addresses = [local.lb_b_public_ip]
    netmask      = sakura_internet.pub.netmask
    gateway      = sakura_internet.pub.gateway
  }

  vip = [{
    vip         = local.lb_b_vip
    port        = var.dsr_lb_port
    delay_loop  = 10
    description = "api"

    # TODO: ヘルスチェックを HTTP に切り替える。
    #   現状は TCP 接続確認のみ。TCP チェックでは docker が 8080 を LISTEN して
    #   さえいれば健全と見なされ、DB に繋がらず全リクエストが 500 になっている
    #   ノードにも振り続けてしまう。
    #   cmd/api/main.go の GET /healthz スタブに DB 疎通確認を実装したうえで、
    #   下記を protocol = "http" / path = "/healthz" / status = 200 に変更する。
    server = [
      for i in local.api_node_indexes : {
        ip_address      = local.node_public_ips[i]
        protocol        = "tcp"
        connect_timeout = 5
        retry           = 3
        enabled         = true
      }
    ]
  }]
}
