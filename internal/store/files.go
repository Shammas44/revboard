package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"

	"revboard/internal/diff"
)

// carryOver holds the review state of a previous snapshot, reused by a refresh.
type carryOver struct {
	files map[string]previousFile
	// hunks maps path + "\x00" + content hash to the statuses of matching hunks;
	// entries are consumed as they are reused.
	hunks map[string][]Status
	// fileNotes lists the paths carrying file notes (their files need attention).
	fileNotes map[string]bool
}

type previousFile struct {
	status     Status
	pinned     bool
	changeType string
}

func hunkKey(path, hash string) string { return path + "\x00" + hash }

func hashBody(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// aggregateStatus derives a file status from the statuses of its hunks.
func aggregateStatus(statuses []Status) Status {
	reviewed := 0
	for _, s := range statuses {
		if s == StatusNeedsAttention {
			return StatusNeedsAttention
		}
		if s == StatusReviewed {
			reviewed++
		}
	}
	if reviewed == len(statuses) {
		return StatusReviewed
	}
	return StatusUnreviewed
}

// insertSnapshot stores the files and hunks of d for a session, reusing the
// review state found in carry (may be nil).
func insertSnapshot(ctx context.Context, tx *sql.Tx, sessionID int64, d *diff.Diff, carry *carryOver) error {
	fileStmt, err := tx.PrepareContext(ctx, `INSERT INTO review_files(session_id, position, path, old_path, new_path,
		change_type, is_binary, old_mode, new_mode, similarity, additions, deletions, status, pinned)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer fileStmt.Close()
	hunkStmt, err := tx.PrepareContext(ctx, `INSERT INTO review_hunks(file_id, position, old_start, old_lines,
		new_start, new_lines, section, body, content_hash, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer hunkStmt.Close()

	for i, f := range d.Files {
		path := f.Path()
		bodies := make([]string, len(f.Hunks))
		hashes := make([]string, len(f.Hunks))
		statuses := make([]Status, len(f.Hunks))
		for j, h := range f.Hunks {
			bodies[j] = h.Body()
			hashes[j] = hashBody(bodies[j])
			statuses[j] = StatusUnreviewed
			if carry != nil {
				key := hunkKey(path, hashes[j])
				if prev := carry.hunks[key]; len(prev) > 0 {
					statuses[j] = prev[0]
					carry.hunks[key] = prev[1:]
				}
			}
		}
		status, pinned := StatusUnreviewed, false
		if carry != nil {
			if prev, ok := carry.files[path]; ok {
				pinned = prev.pinned
				if len(f.Hunks) == 0 && prev.changeType == string(f.ChangeType) {
					status = prev.status
				}
			}
		}
		if len(f.Hunks) > 0 {
			status = aggregateStatus(statuses)
		}
		if carry != nil && carry.fileNotes[path] {
			status = StatusNeedsAttention
		}
		res, err := fileStmt.ExecContext(ctx, sessionID, i, path, f.OldPath, f.NewPath, string(f.ChangeType),
			boolToInt(f.IsBinary), f.OldMode, f.NewMode, f.Similarity, f.Additions, f.Deletions, status, boolToInt(pinned))
		if err != nil {
			return fmt.Errorf("insert file %s: %w", path, err)
		}
		fileID, err := res.LastInsertId()
		if err != nil {
			return err
		}
		for j, h := range f.Hunks {
			_, err := hunkStmt.ExecContext(ctx, fileID, j, h.OldStart, h.OldLines, h.NewStart, h.NewLines,
				h.Section, bodies[j], hashes[j], statuses[j])
			if err != nil {
				return fmt.Errorf("insert hunk of %s: %w", path, err)
			}
		}
	}
	return nil
}

// ReplaceSnapshot replaces the diff of a session, carrying over the statuses of
// unchanged hunks (same path and content) and the pins. Notes are anchored by
// path and are not affected.
func (s *Store) ReplaceSnapshot(ctx context.Context, sessionID int64, headSHA string, d *diff.Diff) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		carry := &carryOver{files: map[string]previousFile{}, hunks: map[string][]Status{}, fileNotes: map[string]bool{}}
		notePaths, err := tx.QueryContext(ctx, `SELECT DISTINCT file_path FROM comments
			WHERE session_id = ? AND line = 0 AND file_path <> ''`, sessionID)
		if err != nil {
			return err
		}
		for notePaths.Next() {
			var path string
			if err := notePaths.Scan(&path); err != nil {
				notePaths.Close()
				return err
			}
			carry.fileNotes[path] = true
		}
		notePaths.Close()
		rows, err := tx.QueryContext(ctx, `SELECT path, status, pinned, change_type FROM review_files WHERE session_id = ?`, sessionID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var path string
			var pf previousFile
			if err := rows.Scan(&path, &pf.status, &pf.pinned, &pf.changeType); err != nil {
				rows.Close()
				return err
			}
			carry.files[path] = pf
		}
		rows.Close()
		rows, err = tx.QueryContext(ctx, `SELECT f.path, h.content_hash, h.status FROM review_hunks h
			JOIN review_files f ON f.id = h.file_id WHERE f.session_id = ? ORDER BY f.position, h.position`, sessionID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var path, hash string
			var st Status
			if err := rows.Scan(&path, &hash, &st); err != nil {
				rows.Close()
				return err
			}
			carry.hunks[hunkKey(path, hash)] = append(carry.hunks[hunkKey(path, hash)], st)
		}
		rows.Close()

		if _, err := tx.ExecContext(ctx, `DELETE FROM review_files WHERE session_id = ?`, sessionID); err != nil {
			return err
		}
		if err := insertSnapshot(ctx, tx, sessionID, d, carry); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE review_sessions SET head_sha = ?,
			captured_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ?`, headSHA, sessionID)
		return err
	})
}

// fileSummaries lists the files of a session (or a single file when fileID != 0).
func fileSummaries(ctx context.Context, q dbtx, sessionID, fileID int64) ([]FileSummary, error) {
	where, arg := "f.session_id = ?", sessionID
	if fileID != 0 {
		where, arg = "f.id = ?", fileID
	}
	rows, err := q.QueryContext(ctx, `SELECT f.id, f.session_id, f.position, f.path, f.old_path, f.new_path,
		f.change_type, f.is_binary, f.old_mode, f.new_mode, f.similarity, f.additions, f.deletions, f.status, f.pinned,
		(SELECT COUNT(*) FROM comments c WHERE c.session_id = f.session_id AND c.file_path = f.path)
		FROM review_files f WHERE `+where+` ORDER BY f.position`, arg)
	if err != nil {
		return nil, err
	}
	files := []FileSummary{}
	index := map[int64]int{}
	for rows.Next() {
		var f FileSummary
		if err := rows.Scan(&f.ID, &f.SessionID, &f.Position, &f.Path, &f.OldPath, &f.NewPath, &f.ChangeType,
			&f.IsBinary, &f.OldMode, &f.NewMode, &f.Similarity, &f.Additions, &f.Deletions, &f.Status, &f.Pinned,
			&f.CommentCount); err != nil {
			rows.Close()
			return nil, err
		}
		f.Hunks = []HunkRef{}
		index[f.ID] = len(files)
		files = append(files, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(files) > 0 {
		m, err := sessionMatcher(ctx, q, files[0].SessionID)
		if err != nil {
			return nil, err
		}
		for i := range files {
			files[i].Excluded = m.Match(files[i].Path)
		}
	}

	rows, err = q.QueryContext(ctx, `SELECT h.id, h.file_id, h.status FROM review_hunks h
		JOIN review_files f ON f.id = h.file_id WHERE `+where+` ORDER BY h.file_id, h.position`, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref HunkRef
		var fid int64
		if err := rows.Scan(&ref.ID, &fid, &ref.Status); err != nil {
			return nil, err
		}
		if i, ok := index[fid]; ok {
			files[i].Hunks = append(files[i].Hunks, ref)
		}
	}
	return files, rows.Err()
}

func fileSummary(ctx context.Context, q dbtx, fileID int64) (FileSummary, error) {
	files, err := fileSummaries(ctx, q, 0, fileID)
	if err != nil {
		return FileSummary{}, err
	}
	if len(files) == 0 {
		return FileSummary{}, notFound("file", fileID)
	}
	return files[0], nil
}

// GetFileDetail returns a file with its parsed hunks and its notes.
func (s *Store) GetFileDetail(ctx context.Context, fileID int64) (FileDetail, error) {
	f, err := fileSummary(ctx, s.db, fileID)
	if err != nil {
		return FileDetail{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, position, old_start, old_lines, new_start, new_lines, section, status, body
		FROM review_hunks WHERE file_id = ? ORDER BY position`, fileID)
	if err != nil {
		return FileDetail{}, err
	}
	defer rows.Close()
	detail := FileDetail{File: f, Hunks: []Hunk{}}
	for rows.Next() {
		var h Hunk
		var body string
		if err := rows.Scan(&h.ID, &h.Position, &h.OldStart, &h.OldLines, &h.NewStart, &h.NewLines, &h.Section, &h.Status, &body); err != nil {
			return FileDetail{}, err
		}
		h.Lines = diff.ParseHunkBody(h.OldStart, h.NewStart, body)
		if h.Lines == nil {
			h.Lines = []diff.Line{}
		}
		detail.Hunks = append(detail.Hunks, h)
	}
	if err := rows.Err(); err != nil {
		return FileDetail{}, err
	}
	detail.Comments, err = listComments(ctx, s.db, f.SessionID, &f.Path)
	return detail, err
}

// SetPinned pins or unpins a file.
func (s *Store) SetPinned(ctx context.Context, fileID int64, pinned bool) (FileSummary, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE review_files SET pinned = ? WHERE id = ?`, boolToInt(pinned), fileID)
	if err != nil {
		return FileSummary{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return FileSummary{}, notFound("file", fileID)
	}
	return fileSummary(ctx, s.db, fileID)
}
