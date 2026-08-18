output "bastion_public_ip" {
  description = "edge のグローバルIP。5台のうちここだけが共有セグメントに接続している。"
  value       = sakura_server.node[0].ip_address
}

output "node_private_ips" {
  description = "各ノードの app セグメント上のプライベートIP。"
  value = {
    for i, s in sakura_server.node : s.name => local.node_private_ips[i]
  }
}

output "db_host" {
  description = "データベースアプライアンスの接続先アドレス。"
  value       = local.db_private_ip
}

output "ssh_commands" {
  description = "各ノードへの SSH コマンド。node-02 以降は edge を踏み台にする。"
  value = {
    for i, s in sakura_server.node : s.name => (
      i == 0
      ? "ssh ubuntu@${sakura_server.node[0].ip_address}"
      : "ssh -J ubuntu@${sakura_server.node[0].ip_address} ubuntu@${local.node_private_ips[i]}"
    )
  }
}

output "switch_id" {
  description = "全ノードが接続する app セグメントの vSwitch ID。"
  value       = sakura_vswitch.app.id
}

output "dsr_lb_vip" {
  description = <<-EOT
    DSR ロードバランサの VIP。api (:8080) はこのアドレス経由で node-02〜05 へ振り分けられる。
    app セグメント内のプライベートアドレスなので、外部からはそのまま到達できない。
  EOT
  value       = local.dsr_lb_vip
}

output "dsr_lb_private_ip" {
  description = "DSR ロードバランサ本体の app セグメント上のアドレス。"
  value       = local.dsr_lb_private_ip
}

output "dsr_lb_real_server_ips" {
  description = "DSR ロードバランサの振り分け先 (node-02〜05)。"
  value       = local.dsr_lb_real_server_ips
}
