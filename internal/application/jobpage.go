package application

import (
	"context"
	"fmt"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

type JobPage struct {
	Jobs     []*domain.Job
	Total    int
	Page     int
	PageSize int
}

type JobPageRepository interface {
	ListPage(context.Context, JobListQuery) (*JobPage, error)
}

func (s *JobService) ListPage(ctx context.Context, q JobListQuery) (*JobPage, error) {
	repo, ok := s.repo.(JobPageRepository)
	if !ok {
		return nil, fmt.Errorf("repository does not support numbered pages")
	}
	return repo.ListPage(ctx, q)
}
