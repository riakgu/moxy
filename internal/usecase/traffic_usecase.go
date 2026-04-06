package usecase

import (
	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/model/converter"
	"github.com/riakgu/moxy/internal/repository"
)

// TrafficUseCase provides traffic stats operations.
type TrafficUseCase struct {
	Log  *logrus.Logger
	Repo *repository.TrafficRepository
}

// NewTrafficUseCase creates a new TrafficUseCase.
func NewTrafficUseCase(log *logrus.Logger, repo *repository.TrafficRepository) *TrafficUseCase {
	return &TrafficUseCase{
		Log:  log,
		Repo: repo,
	}
}

// List returns all traffic entries sorted by connection count descending.
func (uc *TrafficUseCase) List() *model.TrafficListResponse {
	entries := uc.Repo.List()

	responses := make([]model.TrafficEntryResponse, 0, len(entries))
	for _, e := range entries {
		responses = append(responses, converter.TrafficEntryToResponse(e))
	}

	return &model.TrafficListResponse{
		Entries:      responses,
		TotalEntries: len(responses),
	}
}
