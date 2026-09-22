# Tracker behind a Cloudflare Tunnel

A tunnel gives you HTTPS on a public hostname without opening any inbound
port. `cloudflared` dials out to Cloudflare and forwards requests to Tracker
over plain HTTP on your LAN (or loopback).

```
browser ──https──▶ Cloudflare ══tunnel══▶ cloudflared ──http──▶ tracker :3000
```

## 1. Tunnel configuration

Dashboard (Zero Trust → Networks → Tunnels → Public hostname):

| Field           | Value                      |
| --------------- | -------------------------- |
| Public hostname | `track.example.com`        |
| Service         | `http://192.168.1.240:3000`|

or the equivalent `/etc/cloudflared/config.yml`:

```yaml
tunnel: <TUNNEL-UUID>
credentials-file: /etc/cloudflared/<TUNNEL-UUID>.json

ingress:
  - hostname: track.example.com
    service: http://192.168.1.240:3000
  - service: http_status:404
```

## 2. Tracker configuration (`/etc/tracker/tracker.env`)

```ini
TRACKER_ADDR=0.0.0.0:3000
TRACKER_PUBLIC_URL=https://track.example.com/

# Address of the machine that runs cloudflared, as Tracker sees it.
# Same machine  → keep the default (loopback) and prefer TRACKER_ADDR=127.0.0.1:3000
# Other machine → its LAN address:
TRACKER_TRUSTED_PROXIES=127.0.0.0/8,::1/128,192.168.1.10/32
```

`sudo systemctl restart tracker` afterwards.

## What `TRACKER_PUBLIC_URL` does

Tracker never looks at `X-Forwarded-Proto` or `X-Forwarded-Host`. Everything
that depends on the external URL is derived from `TRACKER_PUBLIC_URL`:

* `https://…` ⇒ the session cookie gets the `Secure` attribute, even though
  the last hop (cloudflared → Tracker) is plain HTTP.
* State-changing requests whose `Origin` header is neither the public URL nor
  the `Host` they were sent to are rejected (CSRF protection). Opening the
  app directly via `http://192.168.1.240:3000` keeps working for that reason —
  but note that with an `https` public URL the Secure cookie will not be
  stored over plain HTTP, so sign in through the public hostname.

## Trusted proxy behaviour

Forwarding headers are only used for one thing: the client IP in logs and in
the login rate limiter.

* If the TCP peer is **not** in `TRACKER_TRUSTED_PROXIES`, `CF-Connecting-IP`
  and `X-Forwarded-For` are ignored entirely — an internet client talking to
  the port directly cannot spoof its address.
* If the peer **is** trusted, `CF-Connecting-IP` wins; otherwise
  `X-Forwarded-For` is read right-to-left and the first address that is not
  itself a trusted proxy is used.

If cloudflared runs on another host and you forget to list it, nothing breaks:
logs simply show cloudflared's address for every request, and the login rate
limit is shared by everyone coming through the tunnel.

## Hardening tips

* When the tunnel is the only entrance, bind to loopback or firewall port 3000
  so the LAN cannot bypass Cloudflare (`ufw allow from 192.168.1.10 to any port 3000`).
* Set `TRACKER_ALLOW_REGISTRATION=false` once your accounts exist, or put a
  Cloudflare Access policy in front of the hostname.
