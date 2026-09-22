package story

import (
	"context"
	"strings"

	"trackstar/internal/apperr"
	"trackstar/internal/database"
	"trackstar/internal/database/dbgen"
)

// Epic is a label with a description and computed progress. Stories join an
// epic simply by carrying its label.
type Epic struct {
	ID             int64  `json:"id"`
	ProjectID      int64  `json:"project_id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	TotalPoints    int64  `json:"total_points"`    // estimated feature points, all live stories
	AcceptedPoints int64  `json:"accepted_points"` // of which accepted
	StoryCount     int64  `json:"story_count"`
	AcceptedCount  int64  `json:"accepted_count"`
}

type EpicInput struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

func epicFromRow(r dbgen.Label) Epic {
	return Epic{ID: r.ID, ProjectID: r.ProjectID, Name: r.Name, Description: r.Description}
}

// Epics lists a project's epics with progress.
func (s *Service) Epics(ctx context.Context, projectID int64) ([]Epic, error) {
	labels, err := s.store.ListLabels(ctx, projectID)
	if err != nil {
		return nil, err
	}
	stats, err := s.store.ListLabelStoryStats(ctx, projectID)
	if err != nil {
		return nil, err
	}
	byLabel := map[int64]*Epic{}
	out := []Epic{}
	for _, l := range labels {
		if l.IsEpic {
			out = append(out, epicFromRow(l))
			byLabel[l.ID] = &out[len(out)-1]
		}
	}
	for _, st := range stats {
		e := byLabel[st.LabelID]
		if e == nil {
			continue
		}
		e.StoryCount++
		accepted := State(st.State) == StateAccepted
		if accepted {
			e.AcceptedCount++
		}
		if Type(st.Type) == TypeFeature && st.Estimate.Valid {
			e.TotalPoints += st.Estimate.Int64
			if accepted {
				e.AcceptedPoints += st.Estimate.Int64
			}
		}
	}
	return out, nil
}

// CreateEpic creates an epic, promoting an existing label of the same name.
func (s *Service) CreateEpic(ctx context.Context, projectID int64, in EpicInput) (Epic, error) {
	name, err := cleanLabel(deref(in.Name))
	if err != nil {
		return Epic{}, err
	}
	var row dbgen.Label
	err = s.store.InTx(ctx, func(q dbgen.Querier) error {
		if _, err := q.GetProject(ctx, projectID); err != nil {
			return notFound(err, "project")
		}
		existing, err := q.GetLabelByName(ctx, dbgen.GetLabelByNameParams{ProjectID: projectID, Name: name})
		switch {
		case err == nil && existing.IsEpic:
			return apperr.Conflict("an epic with this name already exists")
		case err == nil:
			row, err = q.UpdateLabel(ctx, dbgen.UpdateLabelParams{ID: existing.ID, Name: name, Description: strings.TrimSpace(deref(in.Description)), IsEpic: true})
			return err
		case database.IsNotFound(err):
			row, err = q.CreateEpic(ctx, dbgen.CreateEpicParams{ProjectID: projectID, Name: name, Description: strings.TrimSpace(deref(in.Description))})
			return err
		default:
			return err
		}
	})
	if err != nil {
		return Epic{}, err
	}
	return s.epicWithProgress(ctx, row)
}

// UpdateEpic renames (renaming the label on every story) or re-describes an epic.
func (s *Service) UpdateEpic(ctx context.Context, id int64, in EpicInput) (Epic, error) {
	var row dbgen.Label
	err := s.store.InTx(ctx, func(q dbgen.Querier) error {
		l, err := q.GetLabel(ctx, id)
		if err != nil || !l.IsEpic {
			return notFound(errOrNotFound(err), "epic")
		}
		name := l.Name
		if in.Name != nil {
			if name, err = cleanLabel(*in.Name); err != nil {
				return err
			}
			if name != l.Name {
				if _, err := q.GetLabelByName(ctx, dbgen.GetLabelByNameParams{ProjectID: l.ProjectID, Name: name}); err == nil {
					return apperr.Conflict("a label with this name already exists")
				} else if !database.IsNotFound(err) {
					return err
				}
			}
		}
		desc := l.Description
		if in.Description != nil {
			desc = strings.TrimSpace(*in.Description)
		}
		row, err = q.UpdateLabel(ctx, dbgen.UpdateLabelParams{ID: id, Name: name, Description: desc, IsEpic: true})
		return err
	})
	if err != nil {
		return Epic{}, err
	}
	return s.epicWithProgress(ctx, row)
}

// DemoteEpic turns an epic back into a plain label; stories keep the label.
func (s *Service) DemoteEpic(ctx context.Context, id int64) error {
	return s.store.InTx(ctx, func(q dbgen.Querier) error {
		l, err := q.GetLabel(ctx, id)
		if err != nil || !l.IsEpic {
			return notFound(errOrNotFound(err), "epic")
		}
		if _, err := q.UpdateLabel(ctx, dbgen.UpdateLabelParams{ID: id, Name: l.Name, Description: "", IsEpic: false}); err != nil {
			return err
		}
		return q.DeleteUnusedLabels(ctx, l.ProjectID)
	})
}

func (s *Service) epicWithProgress(ctx context.Context, row dbgen.Label) (Epic, error) {
	epics, err := s.Epics(ctx, row.ProjectID)
	if err != nil {
		return Epic{}, err
	}
	for _, e := range epics {
		if e.ID == row.ID {
			return e, nil
		}
	}
	return epicFromRow(row), nil
}

func cleanLabel(name string) (string, error) {
	name = strings.ToLower(strings.Join(strings.Fields(name), " "))
	switch {
	case name == "":
		return "", apperr.Invalid("name is required")
	case len(name) > 50:
		return "", apperr.Invalid("name is too long (50 characters max)")
	}
	return name, nil
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func errOrNotFound(err error) error {
	if err != nil {
		return err
	}
	return notFoundErr
}
