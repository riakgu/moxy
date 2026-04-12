package converter

import (
	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
)

func LogEntryToResponse(e entity.LogEntry) model.LogEntry {
	return model.LogEntry{
		Time:      e.Time,
		Level:     e.Level,
		Message:   e.Message,
		Component: e.Component,
		Attrs:     e.Attrs,
	}
}

func LogEntriesToResponse(entries []entity.LogEntry) []model.LogEntry {
	if entries == nil {
		return nil
	}
	result := make([]model.LogEntry, len(entries))
	for i, e := range entries {
		result[i] = LogEntryToResponse(e)
	}
	return result
}
