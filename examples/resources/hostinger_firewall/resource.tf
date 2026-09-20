resource "hostinger_firewall" "web" {
  name = "HTTP and SSH only"

  rule {
    protocol      = "SSH"
    port          = "22"
    source        = "any"
    source_detail = "any"
  }

  rule {
    protocol      = "HTTPS"
    port          = "443"
    source        = "any"
    source_detail = "any"
  }
}
