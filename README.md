Minihub
=======

Minihub is a small self-hosted Git service with a Go backend and a React web frontend. It stores bare repositories on disk and exposes Git smart HTTP through `git http-backend`, so the standard `git` CLI can clone, fetch, and push.

Run locally:

```sh
cd backend
go run ./cmd/minihub
```

Run the frontend dev server in another shell:

```sh
cd frontend
npm install
npm run dev
```

Open `http://localhost:8080`, create a repository, then use the clone URL shown in the UI:

```sh
git clone http://localhost:8080/git/demo.git
```

Configuration:

```sh
MINIHUB_ADDR=:8080              # HTTP listen address
MINIHUB_DATA=./data             # bare repository storage
MINIHUB_DB=./data/minihub.db    # SQLite development database
MINIHUB_FRONTEND=../frontend/dist # built frontend directory
MINIHUB_TLS_CERT=cert.pem       # optional HTTPS certificate
MINIHUB_TLS_KEY=key.pem         # optional HTTPS private key
MINIHUB_SSH_ADDR=:2222          # SSH listen address for minihub-ssh
MINIHUB_SSH_HOST_KEY=./data/ssh_host_key
```

Current capabilities:

- Create and list repositories from the web UI or JSON API.
- Host bare Git repositories on local disk.
- Clone, fetch, and push over HTTP or HTTPS with the normal `git` CLI.
- Browse files and recent commits in the frontend.
- List, switch, create, and delete branches from the frontend and API.
- View commit detail and patch diffs.
- Configure repository descriptions and protected branch names.
- Persist users, sessions, orgs, pull requests, reviews, comments, issues, releases, webhooks, and CI runs in SQLite.
- Open and merge pull requests. Merge attempts use Git merge-tree and return a conflict response instead of changing the target branch when Git cannot merge cleanly.
- Deliver matching webhooks on pull request, release, and CI events.
- Execute `.minihub/ci.sh` from a selected ref and record CI status/log path.
- Serve Git over SSH with `minihub-ssh`; users authenticate with Minihub username/password and repo roles control read/write access.

SQLite is the development database. Matching PostgreSQL schema migrations live in `backend/migrations/postgres` so the production database migration path stays explicit.

Container build:

```sh
docker build -t minihub .
docker run --rm -p 8080:8080 -v minihub-data:/data minihub
```

Run SSH Git service next to the web server:

```sh
cd backend
go run ./cmd/minihub-ssh
git clone ssh://dev@localhost:2222/demo.git
```

The development user is `dev` with an empty password. Create real users and grant repository roles from the UI before using SSH outside local development.

This is intentionally unauthenticated development software. Put it behind a trusted network boundary or add authentication before exposing it on the public internet.
