package project

import (
	"context"

	"trackstar/internal/database/dbgen"
)

// SetStarred stars or unstars a project for one user. Stars are personal:
// they only change the order of that user's project list. Both directions
// are idempotent.
func (s *Service) SetStarred(ctx context.Context, projectID, userID int64, starred bool) error {
	if starred {
		return s.store.StarProject(ctx, dbgen.StarProjectParams{UserID: userID, ProjectID: projectID, Now: s.now().Unix()})
	}
	return s.store.UnstarProject(ctx, dbgen.UnstarProjectParams{UserID: userID, ProjectID: projectID})
}

// StarredIDs returns the set of projects the user has starred.
func (s *Service) StarredIDs(ctx context.Context, userID int64) (map[int64]bool, error) {
	ids, err := s.store.ListStarredProjectIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}
