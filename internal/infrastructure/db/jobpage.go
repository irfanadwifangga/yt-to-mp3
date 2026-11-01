package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// Count and page are read from one snapshot so totals match the displayed rows.
func (r *JobRepository) ListPage(ctx context.Context, q application.JobListQuery) (*application.JobPage, error) {
	if q.Page < 1 || q.PageSize < 1 || q.PageSize > 100 {
		return nil, fmt.Errorf("invalid page bounds")
	}
	where := []string{"1=1"}
	args := []any{}
	if q.Status != "" {
		where = append(where, "status = ?")
		args = append(args, string(q.Status))
	}
	if q.Scope == application.ScopeActive {
		where = append(where, activeStatusFilter)
	}
	if q.Scope == application.ScopeFinished {
		where = append(where, "NOT "+activeStatusFilter)
	}
	if q.Kind != "" {
		where = append(where, "preset_id IN (SELECT id FROM presets WHERE kind = ?)")
		args = append(args, string(q.Kind))
	}
	if q.Query != "" {
		// instr treats percent and underscore as literal characters, unlike LIKE.
		where = append(where, "instr(lower(COALESCE(NULLIF(tag_title, ''), title, source_key)), lower(?)) > 0")
		args = append(args, q.Query)
	}
	filter := " WHERE " + strings.Join(where, " AND ")
	tx, err := r.db.Read().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	page := &application.JobPage{Page: q.Page, PageSize: q.PageSize, Jobs: []*domain.Job{}}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM jobs"+filter, args...).Scan(&page.Total); err != nil {
		return nil, err
	}
	maxPage := max(1, (page.Total+q.PageSize-1)/q.PageSize)
	page.Page = min(page.Page, maxPage)
	pageArgs := append(append([]any{}, args...), q.PageSize, (page.Page-1)*q.PageSize)
	rows, err := tx.QueryContext(ctx, "SELECT "+jobColumns+" FROM jobs"+filter+" ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?", pageArgs...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		page.Jobs = append(page.Jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return page, nil
}
