# Cloud (single-node) deployment

The `cloud` profile is hoster-agnostic. It works on Hetzner Cloud,
DigitalOcean, AWS Lightsail, OVH, bare metal — anywhere you have:

- a public IP
- ports 80 and 443 reachable
- a DNS A record pointing at the public IP

The same compose file works everywhere; only the DNS bootstrap differs.

## What you get

- Caddy fronting the API with **automatic Let's Encrypt TLS**
- Daily restic backups (S3 / B2 / SFTP / local)
- A weekly `restore-verify.sh` (run it from cron)
- prometheus + postgres-exporter scraping the api and postgres
- Postgres bound to a **private docker network only** — never exposed on the host

## 1. Provision a host

Anything with 2 vCPU / 4 GB RAM / 40 GB SSD is enough. Examples:

| Provider           | Smallest fit                                |
|--------------------|---------------------------------------------|
| Hetzner Cloud      | `cax11` (ARM, 2 vCPU, 4 GB)                 |
| DigitalOcean       | basic-2vcpu-2gb (or 4 GB for headroom)      |
| AWS Lightsail      | `2 vCPU / 4 GB`                             |
| Bare metal         | any Pi 5 / mini PC                          |

Install Docker and add your user to the `docker` group:

```bash
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker $USER
```

## 2. Point DNS at the host

Create an A record:

```
db.example.com.   IN  A   203.0.113.42
```

Wait for it to propagate (`dig db.example.com`).

## 3. Configure secrets

```bash
git clone https://github.com/fheinfling/agentic-coop-db.git
cd agentic-coop-db
mkdir -p deploy/secrets
head -c 32 /dev/urandom | base64 > deploy/secrets/postgres_password.txt
head -c 32 /dev/urandom | base64 > deploy/secrets/restic_password.txt
chmod 600 deploy/secrets/*.txt
```

Copy the cloud env example and edit it:

```bash
cp deploy/env/.env.cloud.example deploy/.env
$EDITOR deploy/.env
```

Set at least:

```bash
DOMAIN=db.example.com
EMAIL=ops@example.com
RESTIC_REPOSITORY=s3:s3.amazonaws.com/your-bucket
AWS_ACCESS_KEY_ID=...
AWS_SECRET_ACCESS_KEY=...
```

## 4. Bring it up

```bash
make up-cloud
```

Watch the logs:

```bash
docker compose -p agentcoopdb -f deploy/compose.yml -f deploy/compose.cloud.yml logs -f caddy
```

Caddy will request a certificate from Let's Encrypt on first boot. Once the
certificate is issued, the API is reachable at `https://db.example.com/`.

## 5. Mint your first key

```bash
docker compose -p agentcoopdb exec api /app/agentic-coop-db-server -version
docker compose -p agentcoopdb exec -e DATABASE_URL='postgres://agentcoopdb_owner@postgres/agentcoopdb?sslmode=disable' \
  api sh -c './scripts/gen-key.sh default dbadmin'
```

Or, if you have the host repo: run `./scripts/gen-key.sh` against the
exposed host (note: postgres is NOT exposed in the cloud profile, so use
the in-container path above).

## 6. Backup verification

```bash
docker compose -p agentcoopdb run --rm backup /backup/restore-verify.sh
```

Schedule this weekly via host cron.

## Provider-specific notes

### Hetzner Cloud

- Open ports 80, 443 in the firewall.
- ARM (`cax11`) works because the api image is multi-arch.

### DigitalOcean

- Use the "basic" plan; the cheap plans throttle CPU which makes argon2id
  feel slow on first verify.

### AWS Lightsail

- Lightsail's static IP service is free for an attached instance — use it,
  otherwise the IP changes on restart.

### Bare metal

- Make sure ports 80 and 443 are forwarded to the host on your router.
- Use a dynamic DNS service if you don't have a static IP.

## Operational checklist

- [ ] DNS A record points at the host
- [ ] `deploy/.env` has `DOMAIN`, `EMAIL`, `RESTIC_REPOSITORY`
- [ ] `deploy/secrets/*.txt` exist with mode 0600
- [ ] First admin key minted and stored in your password manager
- [ ] `restore-verify.sh` scheduled weekly
- [ ] `/metrics` is firewalled to your prometheus host (or accept it stays internal)
