package canvas

import (
	"errors"
	"time"

	"yingce/backend/internal/kernel"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

type LegacyShareMigrationFailure struct {
	ShareID string `json:"shareId"`
	Reason  string `json:"reason"`
}

type LegacyShareMigrationResult struct {
	Migrated int                           `json:"migrated"`
	Skipped  int                           `json:"skipped"`
	Next     string                        `json:"next,omitempty"`
	Failures []LegacyShareMigrationFailure `json:"failures"`
}

// MigrateLegacyCanvasShares freezes the last saved state available at the time
// of migration; historical share-time state cannot be reconstructed. Each
// share is committed independently and failed records are left untouched.
func (s *Service) MigrateLegacyCanvasShares(after string, limit int) (LegacyShareMigrationResult, error) {
	result := LegacyShareMigrationResult{Failures: []LegacyShareMigrationFailure{}}
	shares, err := s.repo.LegacyCanvasShares(after, limit)
	if err != nil { return result, err }
	for _, share := range shares {
		result.Next = share.ID
		project, err := s.repo.CanvasProjectForUser(share.UserID, share.ProjectID)
		if err == nil {
			var prepared shareSnapshot
			prepared, err = prepareShareSnapshot(project)
			if err == nil {
				snapshot := &model.CanvasShareSnapshot{ID: kernel.NewID(), ShareID: share.ID, UserID: share.UserID, CanvasID: share.ProjectID, SourceRevision: prepared.Project.Revision, Title: prepared.Project.Title, PayloadJSON: prepared.Project.PayloadJSON, CreatedAt: time.Now()}
				err = s.repo.PublishLegacyCanvasShareSnapshot(share.UserID, snapshot, prepared.ResourceIDs)
			}
		}
		if errors.Is(err, repository.ErrShareSnapshotAlreadyMigrated) { result.Skipped++; continue }
		if err != nil { result.Failures = append(result.Failures, LegacyShareMigrationFailure{ShareID:share.ID, Reason:err.Error()}); continue }
		result.Migrated++
	}
	return result, nil
}
