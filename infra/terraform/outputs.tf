output "bastion_public_ip" {
  description = "node-01 のグローバルIP。5台のうちここだけが共有セグメントに接続している。"
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
  description = "各ノードへの SSH コマンド。node-02 以降は node-01 を踏み台にする。"
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
