# Examples

## Web server

Daily backups of configuration and web roots, with one month of retention and a Healthchecks.io ping:

```yaml
plans:
  - name: web
    schedule: "0 3 * * *"
    sources:
      - type: file
        path: /etc/nginx
      - type: file
        path: /var/www
        exclude: ["*.log", "cache/", "tmp/"]
    destination:
      type: s3
      bucket: my-backups
      prefix: /web
      endpoint: s3.amazonaws.com
      region: us-east-1
      access-key: ${AWS_ACCESS_KEY_ID}
      secret-key: ${AWS_SECRET_ACCESS_KEY}
    retention:
      keep-last: 30
      keep-daily: 7
      keep-weekly: 4
    hooks:
      post-backup:
        - "curl -fsS -m 10 --retry 5 -o /dev/null https://hc-ping.com/my-uuid"
      on-failure:
        - "curl -fsS -m 10 -o /dev/null https://hc-ping.com/my-uuid/fail"
```

## Database + application data

A single plan backing up a PostgreSQL database and its data directory:

```yaml
plans:
  - name: app
    schedule: "0 */6 * * *"          # every 6 hours
    sources:
      - type: database
        adapter: postgres
        dsn: "postgres://backup:${DB_PASSWORD}@localhost:5432/mydb"
        dump-tool: pg_dump
      - type: file
        path: /var/lib/myapp
    destination:
      type: s3
      bucket: my-backups
      prefix: /app
      endpoint: s3.amazonaws.com
      region: us-east-1
      access-key: ${AWS_ACCESS_KEY_ID}
      secret-key: ${AWS_SECRET_ACCESS_KEY}
    encryption:
      passphrase: ${BACKUP_PASSPHRASE}
    retention:
      keep-last: 40
      keep-daily: 14
      keep-weekly: 8
      keep-monthly: 24
```

## Docker volume

Snapshot a named volume with `docker run --rm`:

```yaml
plans:
  - name: docker
    schedule: "30 1 * * *"
    sources:
      - type: docker
        volume: myapp_data
    destination:
      type: s3
      bucket: my-backups
      prefix: /docker
      endpoint: s3.amazonaws.com
      region: us-east-1
      access-key: ${AWS_ACCESS_KEY_ID}
      secret-key: ${AWS_SECRET_ACCESS_KEY}
    retention:
      keep-last: 14
```

## Kubernetes PVC

Back up a PersistentVolumeClaim, optionally via a CSI volume snapshot:

```yaml
plans:
  - name: k8s
    schedule: "0 2 * * *"
    sources:
      - type: kubernetes
        pvc: data-pvc
        snapshot: true
    destination:
      type: s3
      bucket: my-backups
      prefix: /k8s
      endpoint: s3.amazonaws.com
      region: us-east-1
      access-key: ${AWS_ACCESS_KEY_ID}
      secret-key: ${AWS_SECRET_ACCESS_KEY}
    retention:
      keep-last: 7
```

## Local MinIO (no cloud)

Everything self-hosted - MinIO on the LAN, plain HTTP, and tagged objects:

```yaml
plans:
  - name: homelab
    schedule: "0 4 * * *"
    sources:
      - type: file
        path: /srv
    destination:
      type: s3
      bucket: homelab-backups
      endpoint: minio.lan:9000
      secure: false
      access-key: ${MINIO_ACCESS_KEY}
      secret-key: ${MINIO_SECRET_KEY}
    encryption:
      passphrase: ${BACKUP_PASSPHRASE}
    retention:
      keep-last: 30
      keep-monthly: 12
    tags:
      environment: homelab
```
