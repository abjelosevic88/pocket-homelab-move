# API

Everything under `/api/v1`, signed in with the session cookie or `Authorization: Bearer pmv_…` (Settings → API & shortcuts). Every move belongs to the person who made it; someone else's id answers 404.

## Apps

| | |
|---|---|
| `GET /apps` | Every Pocket app with Move's two links for you (`out`, `in`: `{on, state, reason}`) and, where they are on, what the app gives (`exports`) and takes (`imports`) — Pocket Core's `Portable` (see `docs/move.md` in pocket-homelab-core) |

## Moving out

| | |
|---|---|
| `POST /out` `{"apps": ["notes", "tasks"]}` | Start an archive (answers 201 with it; it is made in the background) |
| `GET /out`, `GET /out/{id}` | Your archives: `status` queued / running / done / failed / expired, per app `results` with the files |
| `GET /out/{id}/download` | The zip (ranges supported) |
| `DELETE /out/{id}` | Remove it |

The zip holds one folder: `manifest.json` (`format: pocket-move/1`, the person, every file with its kind and SHA-256), `README.html` (what each file is, readable offline) and a folder per app with the app's own export (`kind: pocket`) and, unless switched off in Settings, its open-format files.

## Moving in

| | |
|---|---|
| `POST /in?name=<file name>` | The file as the raw body (any size up to `MAX_UPLOAD_BYTES`); answers 201 and starts reading it |
| `GET /in`, `GET /in/{id}` | Your moves; one move comes with its `items`: what was found, its kind, count, the converted file, where it goes (`target`, `path`, `options`), `include`, `status`, the app's answer (`result`) |
| `PUT /in/{id}/items/{item}` | `{"include": true}`, `{"target": "<app> <path> <kind>"}` (one of `options`), `{"extra": {"list": "…"}}` |
| `GET /in/{id}/items/{item}/download` | The converted file (Markdown zip, .ics, .vcf…) |
| `POST /in/{id}/run` | Send every chosen item to its app (202; in the background) |
| `DELETE /in/{id}` | Remove Move's copies |

An item already sent to the same app with the same content shows `done-before` and is not chosen again. Passwords and photos are never sent: they come with `manual`, what to do instead.

## Other

`GET/PUT /settings`, `GET /settings/system`, `POST /notify/test`, `GET /backup` (admin), users and tokens as in every Pocket app. `/healthz`, `/readyz`, `/metrics` (with `METRICS_ENABLED`).

## Events (Pocket Core)

`move.out_ready` (`apps`, `failed`, `bytes`) and `move.in_finished` (`source`, `sent`, `failed`), with `user_email`.
