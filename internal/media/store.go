package media

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/model"
)

// Media is the full media row (api/openapi.yaml Media).
type Media struct {
	model.MediaRef
	Ext          string          `json:"ext"`
	Bytes        int64           `json:"bytes"`
	TakenAt      *string         `json:"taken_at"`
	OriginalName *string         `json:"original_name"`
	Exif         json.RawMessage `json:"exif"` // nil marshals as null
	Context      *string         `json:"context"`
	VariantError *string         `json:"variant_error"`
	CreatedAt    string          `json:"created_at"`
	Links        []Link          `json:"links"`
}

// UploadResult is the POST /api/media response.
type UploadResult struct {
	Media
	Deduplicated bool `json:"deduplicated"`
}

// Link is a media_link row (api/openapi.yaml MediaLink).
type Link struct {
	MediaID     string  `json:"media_id"`
	SubjectType string  `json:"subject_type"`
	SubjectID   string  `json:"subject_id"`
	Role        string  `json:"role"`
	Sort        int     `json:"sort"`
	Caption     *string `json:"caption"`
	CreatedAt   string  `json:"created_at"`
}

const mediaCols = `m.id, m.sha256, m.mime, m.ext, m.bytes, m.width, m.height, m.taken_at,
	m.original_name, m.exif, m.context, m.variant_status, m.variant_error, m.created_at`

type scanner interface{ Scan(dest ...any) error }

func scanMedia(s scanner) (*Media, error) {
	var (
		m                          Media
		id, sha, mime, status      string
		w, h                       sql.NullInt64
		taken, name, exif, ctx, ve sql.NullString
	)
	if err := s.Scan(&id, &sha, &mime, &m.Ext, &m.Bytes, &w, &h, &taken, &name, &exif, &ctx, &status, &ve, &m.CreatedAt); err != nil {
		return nil, err
	}
	m.MediaRef = Ref(id, sha, mime, w, h, status)
	m.TakenAt = nullStr(taken)
	m.OriginalName = nullStr(name)
	if exif.Valid {
		m.Exif = json.RawMessage(exif.String)
	}
	m.Context = nullStr(ctx)
	m.VariantError = nullStr(ve)
	m.Links = []Link{}
	return &m, nil
}

func nullStr(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	v := s.String
	return &v
}

// loadMedia loads one media row by id (or by sha256 when bySha) with links.
func loadMedia(ctx context.Context, q db.Querier, key string, bySha bool) (*Media, error) {
	col := "m.id"
	if bySha {
		col = "m.sha256"
	}
	m, err := scanMedia(q.QueryRowContext(ctx, `SELECT `+mediaCols+` FROM media m WHERE `+col+` = ?`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpapi.NotFound("media %s not found", key)
	}
	if err != nil {
		return nil, err
	}
	if err := attachLinks(ctx, q, []*Media{m}); err != nil {
		return nil, err
	}
	return m, nil
}

// attachLinks fills Links for each media row.
func attachLinks(ctx context.Context, q db.Querier, ms []*Media) error {
	if len(ms) == 0 {
		return nil
	}
	byID := make(map[string]*Media, len(ms))
	ids := make([]string, 0, len(ms))
	for _, m := range ms {
		byID[m.ID] = m
		ids = append(ids, m.ID)
	}
	rows, err := q.QueryContext(ctx, `SELECT media_id, subject_type, subject_id, role, sort, caption, created_at
		FROM media_link WHERE media_id IN (`+db.Placeholders(len(ids))+`)
		ORDER BY media_id, subject_type, subject_id`, db.Args(ids)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return err
		}
		if m := byID[l.MediaID]; m != nil {
			m.Links = append(m.Links, *l)
		}
	}
	return rows.Err()
}

func scanLink(s scanner) (*Link, error) {
	var l Link
	var caption sql.NullString
	if err := s.Scan(&l.MediaID, &l.SubjectType, &l.SubjectID, &l.Role, &l.Sort, &caption, &l.CreatedAt); err != nil {
		return nil, err
	}
	l.Caption = nullStr(caption)
	return &l, nil
}
