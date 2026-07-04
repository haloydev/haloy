# Verifying the haloyd / haloy-proxy split on a server

The server runs two daemons:

- `haloyd` (control plane): Docker discovery, health checks, ACME certificates, the API. Binds only loopback ports (`127.0.0.1:9922` for the API, `127.0.0.1:8080` for ACME challenges).
- `haloy-proxy` (data plane): binds ports 80/443 and serves all traffic. Receives routing snapshots from haloyd over `/var/lib/haloy/proxy/haloy-proxy.sock` and boots from `/var/lib/haloy/proxy/snapshot.json` when haloyd is down.

The point of the split: upgrading or restarting haloyd never interrupts traffic. Use this checklist after changes to the proxy, the wire format, the client, or the install/upgrade scripts.

## 1. Fresh install serves traffic

```sh
curl -fsSL https://sh.haloy.dev/install-haloyd.sh | API_DOMAIN=... sh
haloy deploy   # from your machine, any test app
curl -s https://app.example.com/   # expect the app
```

Both services should be running: `systemctl status haloyd haloy-proxy`.

## 2. haloyd restart drops zero requests

In one shell on any machine:

```sh
while sleep 0.2; do curl -so /dev/null -w '%{http_code}\n' https://app.example.com; done
```

On the server: `systemctl restart haloyd`. Expect only 200s in the curl loop. Also keep a websocket or `haloy logs` stream open across the restart; it should stay connected.

## 3. Proxy stands alone

```sh
systemctl stop haloyd
curl -s https://app.example.com/   # still serves
systemctl start haloyd
```

Harder variant: disable haloyd and reboot the box. haloy-proxy alone must restore service from the snapshot file and on-disk certificates (`journalctl -u haloy-proxy` should log "Routing snapshot applied" with its age).

## 4. Certificate reload crosses the process boundary

Trigger a renewal (staging via `haloyd serve --debug`, or change the API domain). haloyd logs the cert update signal, and the proxy must serve the new certificate:

```sh
openssl s_client -connect app.example.com:443 -servername app.example.com </dev/null 2>/dev/null | openssl x509 -noout -dates
```

## 5. Migration from a pre-split install

On a server running a single-process haloyd (pre-split), run the curl loop from step 2 and then:

```sh
curl -fsSL https://sh.haloy.dev/upgrade-server.sh | sh
```

Expect a gap of a few seconds at most (ports move from old haloyd to haloy-proxy), then full recovery once haloyd rediscovers containers. Verify both services are active and `haloy server version` reports both components.

A failed migration rolls back completely: the proxy binary and service are removed, the old haloyd binary and service definition are restored (including its bind capability on non-systemd installs), and haloyd is restarted. Re-running the script afterwards detects the pre-split install again and re-attempts the migration.

## 6. Status and reconciliation

```sh
curl -s --unix-socket /var/lib/haloy/proxy/haloy-proxy.sock http://proxy/v1/status
```

`config_hash` must match what haloyd last pushed (haloyd re-pushes within ~5s whenever they diverge, e.g. after a proxy restart). `loaded_from` is `socket` in steady state, `snapshot-file` right after a proxy restart while haloyd is down, `empty` only on a fresh install.

## Upgrading components

- `upgrade-server.sh` upgrades haloyd only; traffic is untouched.
- `upgrade-server.sh --component=proxy` upgrades haloy-proxy (brief restart; rare, the proxy is intentionally small and stable).
- Schema compatibility: the proxy rejects snapshots with a newer `schema_version` (HTTP 409) and keeps its last config. When a schema bump ships, upgrade haloy-proxy before haloyd.
