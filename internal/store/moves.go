package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

// ---- moving out ----

const outCols = `id, user_id, status, apps, results, file, size_bytes, error, created_at, finished_at, expires_at`

func scanOut(row interface{ Scan(...any) error }) (*MoveOut, error) {
	var m MoveOut
	var apps, results string
	if err := row.Scan(&m.ID, &m.UserID, &m.Status, &apps, &results, &m.File, &m.SizeBytes, &m.Error, &m.CreatedAt, &m.FinishedAt, &m.ExpiresAt); err != nil {
		return nil, notFound(err)
	}
	_ = json.Unmarshal([]byte(apps), &m.Apps)
	_ = json.Unmarshal([]byte(results), &m.Results)
	if m.Apps == nil {
		m.Apps = []string{}
	}
	if m.Results == nil {
		m.Results = []OutAppResult{}
	}
	return &m, nil
}

// CreateMoveOut queues an archive for a person.
func (s *Store) CreateMoveOut(ctx context.Context, uid int64, apps []string) (*MoveOut, error) {
	b, _ := json.Marshal(apps)
	res, err := s.DB.ExecContext(ctx, `INSERT INTO moves_out (user_id, apps) VALUES (?, ?)`, uid, string(b))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetMoveOut(ctx, uid, id)
}

// GetMoveOut returns one of the person's archives.
func (s *Store) GetMoveOut(ctx context.Context, uid, id int64) (*MoveOut, error) {
	return scanOut(s.DB.QueryRowContext(ctx, `SELECT `+outCols+` FROM moves_out WHERE id = ? AND user_id = ?`, id, uid))
}

// ListMoveOut lists the person's archives, newest first.
func (s *Store) ListMoveOut(ctx context.Context, uid int64) ([]MoveOut, error) {
	return s.listOut(ctx, `SELECT `+outCols+` FROM moves_out WHERE user_id = ? ORDER BY id DESC LIMIT 100`, uid)
}

func (s *Store) listOut(ctx context.Context, q string, args ...any) ([]MoveOut, error) {
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MoveOut{}
	for rows.Next() {
		m, err := scanOut(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// SaveMoveOut writes the progress of an archive.
func (s *Store) SaveMoveOut(ctx context.Context, m *MoveOut) error {
	results, _ := json.Marshal(m.Results)
	_, err := s.DB.ExecContext(ctx, `UPDATE moves_out SET status = ?, results = ?, file = ?, size_bytes = ?, error = ?, finished_at = ?, expires_at = ? WHERE id = ? AND user_id = ?`,
		m.Status, string(results), m.File, m.SizeBytes, m.Error, m.FinishedAt, m.ExpiresAt, m.ID, m.UserID)
	return err
}

// DeleteMoveOut removes an archive's row (the caller removes the file).
func (s *Store) DeleteMoveOut(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM moves_out WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UnfinishedMoveOut lists archives a restart interrupted (every person).
func (s *Store) UnfinishedMoveOut(ctx context.Context) ([]MoveOut, error) {
	return s.listOut(ctx, `SELECT `+outCols+` FROM moves_out WHERE status IN ('queued', 'running') ORDER BY id`)
}

// ExpiredMoveOut lists finished archives past their expiry (every person).
func (s *Store) ExpiredMoveOut(ctx context.Context, now string) ([]MoveOut, error) {
	return s.listOut(ctx, `SELECT `+outCols+` FROM moves_out WHERE expires_at != '' AND expires_at < ? AND file != ''`, now)
}

// ---- moving in ----

const inCols = `id, user_id, status, name, source, source_name, file, size_bytes, error, notes, created_at, finished_at`

func scanIn(row interface{ Scan(...any) error }) (*MoveIn, error) {
	var m MoveIn
	var notes string
	if err := row.Scan(&m.ID, &m.UserID, &m.Status, &m.Name, &m.Source, &m.SourceName, &m.File, &m.SizeBytes, &m.Error, &notes, &m.CreatedAt, &m.FinishedAt); err != nil {
		return nil, notFound(err)
	}
	_ = json.Unmarshal([]byte(notes), &m.Notes)
	if m.Notes == nil {
		m.Notes = []string{}
	}
	return &m, nil
}

// CreateMoveIn starts a move-in for an upload.
func (s *Store) CreateMoveIn(ctx context.Context, uid int64, name string) (*MoveIn, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO moves_in (user_id, name) VALUES (?, ?)`, uid, name)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetMoveIn(ctx, uid, id)
}

// GetMoveIn returns one of the person's move-ins (without items).
func (s *Store) GetMoveIn(ctx context.Context, uid, id int64) (*MoveIn, error) {
	return scanIn(s.DB.QueryRowContext(ctx, `SELECT `+inCols+` FROM moves_in WHERE id = ? AND user_id = ?`, id, uid))
}

// ListMoveIn lists the person's move-ins, newest first.
func (s *Store) ListMoveIn(ctx context.Context, uid int64) ([]MoveIn, error) {
	return s.listIn(ctx, `SELECT `+inCols+` FROM moves_in WHERE user_id = ? ORDER BY id DESC LIMIT 100`, uid)
}

func (s *Store) listIn(ctx context.Context, q string, args ...any) ([]MoveIn, error) {
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MoveIn{}
	for rows.Next() {
		m, err := scanIn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// SaveMoveIn writes a move-in's state.
func (s *Store) SaveMoveIn(ctx context.Context, m *MoveIn) error {
	notes, _ := json.Marshal(m.Notes)
	_, err := s.DB.ExecContext(ctx, `UPDATE moves_in SET status = ?, name = ?, source = ?, source_name = ?, file = ?, size_bytes = ?, error = ?, notes = ?, finished_at = ? WHERE id = ? AND user_id = ?`,
		m.Status, m.Name, m.Source, m.SourceName, m.File, m.SizeBytes, m.Error, string(notes), m.FinishedAt, m.ID, m.UserID)
	return err
}

// DeleteMoveIn removes a move-in and its items.
func (s *Store) DeleteMoveIn(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM moves_in WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UnfinishedMoveIn lists move-ins a restart interrupted (every person).
func (s *Store) UnfinishedMoveIn(ctx context.Context) ([]MoveIn, error) {
	return s.listIn(ctx, `SELECT `+inCols+` FROM moves_in WHERE status IN ('uploading', 'scanning', 'running') ORDER BY id`)
}

// OldMoveIn lists move-ins finished before the time, whose files are still on disk.
func (s *Store) OldMoveIn(ctx context.Context, before string) ([]MoveIn, error) {
	return s.listIn(ctx, `SELECT `+inCols+` FROM moves_in WHERE created_at < ? AND status IN ('ready', 'done', 'failed') AND file != ''`, before)
}

// ---- items ----

const itemCols = `id, move_id, user_id, label, detail, kind, count, file, file_name, bytes, sha256, target, path, extra, include, manual, status, result, error, sent_at, sort`

func scanItem(row interface{ Scan(...any) error }) (*Item, error) {
	var it Item
	var extra, result string
	var include int
	if err := row.Scan(&it.ID, &it.MoveID, &it.UserID, &it.Label, &it.Detail, &it.Kind, &it.Count, &it.File, &it.FileName, &it.Bytes, &it.SHA256,
		&it.Target, &it.Path, &extra, &include, &it.Manual, &it.Status, &result, &it.Error, &it.SentAt, &it.Sort); err != nil {
		return nil, notFound(err)
	}
	it.Include = include == 1
	_ = json.Unmarshal([]byte(extra), &it.Extra)
	if it.Extra == nil {
		it.Extra = map[string]string{}
	}
	if json.Valid([]byte(result)) {
		it.Result = json.RawMessage(result)
	}
	return &it, nil
}

// AddItem stores a found item.
func (s *Store) AddItem(ctx context.Context, it *Item) error {
	extra, _ := json.Marshal(it.Extra)
	if it.Status == "" {
		it.Status = "waiting"
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO move_items (move_id, user_id, label, detail, kind, count, file, file_name, bytes, sha256, target, path, extra, include, manual, status, sort)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		it.MoveID, it.UserID, it.Label, it.Detail, it.Kind, it.Count, it.File, it.FileName, it.Bytes, it.SHA256, it.Target, it.Path, string(extra), boolInt(it.Include), it.Manual, it.Status, it.Sort)
	if err != nil {
		return err
	}
	it.ID, _ = res.LastInsertId()
	return nil
}

// Items lists a move-in's items in order.
func (s *Store) Items(ctx context.Context, uid, moveID int64) ([]Item, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+itemCols+` FROM move_items WHERE user_id = ? AND move_id = ? ORDER BY sort, id`, uid, moveID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// GetItem returns one of the person's items.
func (s *Store) GetItem(ctx context.Context, uid, id int64) (*Item, error) {
	return scanItem(s.DB.QueryRowContext(ctx, `SELECT `+itemCols+` FROM move_items WHERE id = ? AND user_id = ?`, id, uid))
}

// SaveItem writes an item's choice and outcome.
func (s *Store) SaveItem(ctx context.Context, it *Item) error {
	extra, _ := json.Marshal(it.Extra)
	result := "null"
	if len(it.Result) > 0 && json.Valid(it.Result) {
		result = string(it.Result)
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE move_items SET target = ?, path = ?, extra = ?, include = ?, status = ?, result = ?, error = ?, sent_at = ?, file = ? WHERE id = ? AND user_id = ?`,
		it.Target, it.Path, string(extra), boolInt(it.Include), it.Status, result, it.Error, it.SentAt, it.File, it.ID, it.UserID)
	return err
}

// SentBefore says when the person last sent this exact file to this app, "" = never.
func (s *Store) SentBefore(ctx context.Context, uid int64, target, sha string, notID int64) string {
	var at sql.NullString
	_ = s.DB.QueryRowContext(ctx, `SELECT MAX(sent_at) FROM move_items WHERE user_id = ? AND target = ? AND sha256 = ? AND status = 'sent' AND id != ?`, uid, target, sha, notID).Scan(&at)
	return at.String
}
