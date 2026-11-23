# Blue-green deployment demo

The **blue client** calls a todo API through an nginx proxy. Behind the proxy
are **two identical green servers**, `green-1` and `green-2`. Only one gets
traffic at a time. To release a new version you deploy to the *idle* green
server, health-check it, then flip nginx over - the live server is never
touched, so users see no downtime.

```
                         +--> green-1 :8080 (live) --+
blue client --> nginx ---+                           +--> postgres
   :3001        :8080    +--> green-2 :8080 (idle) --+
                       (nginx/active.conf picks one)
```

The client calls the API every 500 ms and shows UP/DOWN, latency, and which
green server answered each request.

## Run it

```bash
docker compose down -v      # once, if you ran an earlier version
./deploy.sh start           # db + green-1 + nginx + blue client
```

- API: http://localhost:8080/todos (also `/health`, `/version`)
- Blue client: http://localhost:3001

## Deploy a new version

Change something in `server/` (e.g. a response field), then:

```bash
./deploy.sh deploy v2
```

1. Builds and starts the idle green server with `VERSION=v2`.
2. Waits until its Docker healthcheck passes (it can reach Postgres).
3. Rewrites `nginx/active.conf`, runs `nginx -t`, then `nginx -s reload` (graceful).
4. Waits a few seconds to drain, then stops the old server (kept for rollback).

If step 2 fails, the new server is stopped and traffic never leaves the live one.

```bash
./deploy.sh status      # which green server is live
./deploy.sh rollback    # flip back to the previous one
curl localhost:8080/version
```

Watch the client while you deploy: the "serving" label changes from
`green-1` to `green-2` and the graph should stay free of red bands.

## Database changes

Both green servers share one Postgres, and during a switch the old and new
code run against the same schema. Schema changes must therefore be
backward-compatible (add columns/tables first; remove or rename only in a
later release).

## Limitations (it's a demo)

- Hard-coded DB credentials; the client runs `vite dev`.
- nginx's config is rewritten by a script; production setups usually use
  Traefik/Envoy/a cloud load balancer API instead.
- `deploy.sh start` is for first start; use `deploy` afterwards.
