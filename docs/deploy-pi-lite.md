# Raspberry Pi (pi-lite) profile

The `pi-lite` profile is tuned for a Raspberry Pi 4/5 with 4 GB RAM running
Raspberry Pi OS or any 64-bit Linux. The same image works on any ARM64 host.

## Pre-requisites

```bash
sudo systemctl enable --now docker
docker run --rm hello-world
```

## Bring it up

```bash
make up-pi
```

The `pi-lite` overrides:

- `shared_buffers=64MB`, `effective_cache_size=192MB`, `work_mem=2MB`,
  `max_connections=40`
- API pool size 8 (down from 20)
- Per-key rate limit 30 req/s (down from 60)
- Memory ceiling per container so a hot query cannot OOM the Pi

## Verifying

```bash
curl http://localhost:8080/healthz
./scripts/gen-key.sh default dbadmin
```

If you have `mosquitto` or `prometheus-node-exporter` already running on
the Pi, edit `deploy/compose.pi-lite.yml` to bind a different host port to
avoid the collision.
