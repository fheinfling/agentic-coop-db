# Docker Swarm

`deploy/stack.swarm.yml` is the swarm-native deployment file. It uses
swarm secrets and external networks so the same stack can be reused across
multiple swarm services.

## Pre-requisites

```bash
docker swarm init
docker network create --driver overlay --attachable agentic-coop-db_frontend
docker network create --driver overlay --internal   agentic-coop-db_backend

printf '%s' "$(head -c 32 /dev/urandom | base64)" \
  | docker secret create agentcoopdb_postgres_password -

printf '%s' "$(head -c 32 /dev/urandom | base64)" \
  | docker secret create agentcoopdb_restic_password -
```

## Deploy

```bash
make swarm-deploy
# or
docker stack deploy -c deploy/stack.swarm.yml agentcoopdb
```

## Updating

```bash
docker service update --image ghcr.io/fheinfling/agentic-coop-db-server:0.1.1 agentcoopdb_api
```

## Constraints

The stack pins postgres to a manager node so its volume stays on a known
host. For multi-node swarms, use a shared volume driver (e.g. NFS or a
managed block storage CSI) and remove the placement constraint.

## Logging

Add a `logging:` block per service to forward to your log aggregator.
Default is the docker daemon's json-file driver.
