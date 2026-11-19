# Backup

One SQLite file: `DATA_DIR/pocket-move.db` (WAL mode). Take a consistent copy with `pocket-move backup` (writes `DATA_DIR/backups/pocket-move-<time>.db` with `VACUUM INTO`) or Settings → Backup → Download database (admin). Back up that copy, not the live file. `DATA_DIR/core.token*` is the Pocket Core registration.

Restore: stop the app, replace `pocket-move.db` (remove `-wal`/`-shm` files), start.

`DATA_DIR/files/` holds the archives and uploads; they are short-lived (removed after `KEEP_DAYS`) and need no backup. Leave them out of nightly backups.
