# oldtimer

oldtimer is an OCI image building on top of Bazzite/ublue-os images.

## Aeons CI runner credential

Aeons CI runs on ARC in the local k3d cluster; this image carries no runner
bootstrap. The only host-side piece is the runner GitHub App's private key,
which Aeons' `infra/kubernetes/arc/import-github-app.py` decrypts into the ARC
secret.

The App is installed only on `FullPotatoStudios/Aeons` with repository
`Administration: read and write` and no other permissions. Encrypt a downloaded
private key, then remove the plaintext PEM:

```bash
sudo install -d -o root -g root -m 0700 /etc/credstore.encrypted
sudo systemd-creds encrypt --name=github-app-key github-app.pem \
  /etc/credstore.encrypted/github-app-key
```
