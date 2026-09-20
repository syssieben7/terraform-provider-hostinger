resource "hostinger_firewall" "web" {
  name = "HTTP and SSH only"

  rule {
    protocol      = "SSH"
    port          = "22"
    source        = "any"
    source_detail = "any"
  }
}

resource "hostinger_firewall_attachment" "web" {
  firewall_id        = hostinger_firewall.web.id
  virtual_machine_id = hostinger_vps.example.id
}
