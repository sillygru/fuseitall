# FuseItAll (Mac)

Free and open-source Android <-> Mac link (AGPL-3.0). BASE milestone:
QR pairing + manual ping.

```sh
task dev:mac     # run with hot reload (repo root) — or: wails3 dev
task build:mac   # production build -> bin/fuseitall
```

Frontend: Svelte 5 + Tailwind v4 (`frontend/`, pnpm). Backend: thin Go
adapter (`backend/`) over `packages/core`. Conventions live in the repo
root `AGENTS.md`.
