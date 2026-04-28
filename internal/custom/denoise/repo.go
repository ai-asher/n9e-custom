package denoise

import (
	"encoding/json"
	"time"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/ctx"
	"gorm.io/gorm"
)

// Repo encapsulates DB access for incidents. Kept as a struct (not a bag of
// package-level functions) so it can be swapped for a fake in tests.
type Repo struct {
	ctx *ctx.Context
}

func NewRepo(ctx *ctx.Context) *Repo {
	return &Repo{ctx: ctx}
}

// FindActiveIncident looks up an open incident matching (rule_id, incident_key)
// whose last_event_at is within `windowSec` seconds of `now`. Returns nil if
// no such incident exists — the caller should then create a fresh one.
//
// Why bound by last_event_at rather than first_event_at:
//
//	A "rolling" window matches operator intuition — as long as duplicates keep
//	coming, the same incident keeps absorbing them. Once the alert truly stops
//	for `windowSec` seconds, the incident closes and the next event opens a
//	fresh one. This avoids gigantic incidents that span days during outages.
func (r *Repo) FindActiveIncident(ruleId int64, incidentKey string, windowSec, now int64) (*customModels.CustomIncident, error) {
	var inc customModels.CustomIncident
	cutoff := now - windowSec
	err := models.DB(r.ctx).
		Where("rule_id = ? AND incident_key = ? AND status = ? AND last_event_at >= ?",
			ruleId, incidentKey, customModels.IncidentStatusOpen, cutoff).
		Order("id DESC").
		Limit(1).
		Find(&inc).Error
	if err != nil {
		return nil, err
	}
	if inc.Id == 0 {
		return nil, nil
	}
	return &inc, nil
}

// CreateIncident persists a new incident derived from the first event that
// hits the given (rule, key). The caller is responsible for filling all
// non-default fields; this method only stamps timestamps.
func (r *Repo) CreateIncident(inc *customModels.CustomIncident) error {
	if inc.FirstEventAt == 0 {
		inc.FirstEventAt = time.Now().Unix()
	}
	if inc.LastEventAt == 0 {
		inc.LastEventAt = inc.FirstEventAt
	}
	if inc.EventCount == 0 {
		inc.EventCount = 1
	}
	inc.Status = customModels.IncidentStatusOpen
	return models.DB(r.ctx).Create(inc).Error
}

// AppendEvent records that another event has been merged into an existing
// incident: bumps event_count, advances last_event_at, optionally upgrades
// severity, and writes the join-table row.
//
// All updates run in a single transaction so the join row and the counter
// can never disagree (e.g. a counter increment without a corresponding event
// row would be silently lost).
func (r *Repo) AppendEvent(incidentId int64, event *models.AlertCurEvent, upgradeSeverity bool) error {
	now := time.Now().Unix()
	return models.DB(r.ctx).Transaction(func(tx *gorm.DB) error {
		updates := map[string]interface{}{
			"last_event_at": now,
			"event_count":   gorm.Expr("event_count + 1"),
		}
		if upgradeSeverity {
			// Lower severity number = higher priority in N9e (1=critical).
			// Only overwrite when incoming severity is strictly more severe.
			updates["severity"] = gorm.Expr(
				"CASE WHEN severity > ? THEN ? ELSE severity END",
				event.Severity, event.Severity,
			)
		}

		if err := tx.Model(&customModels.CustomIncident{}).
			Where("id = ?", incidentId).
			Updates(updates).Error; err != nil {
			return err
		}

		link := &customModels.CustomIncidentEvent{
			IncidentId: incidentId,
			EventHash:  event.Hash,
			EventId:    event.Id,
			MergedAt:   now,
		}
		return tx.Create(link).Error
	})
}

// MarkEventRecovered flips the recovered flag on the join row matching
// (incidentId, event.Hash). If, after this update, every linked event is
// recovered, the parent incident is auto-resolved.
//
// Idempotent: calling twice for the same event is harmless.
func (r *Repo) MarkEventRecovered(incidentId int64, eventHash string) error {
	now := time.Now().Unix()
	return models.DB(r.ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&customModels.CustomIncidentEvent{}).
			Where("incident_id = ? AND event_hash = ?", incidentId, eventHash).
			Update("is_recovered", 1).Error; err != nil {
			return err
		}

		var unrecovered int64
		if err := tx.Model(&customModels.CustomIncidentEvent{}).
			Where("incident_id = ? AND is_recovered = 0", incidentId).
			Count(&unrecovered).Error; err != nil {
			return err
		}
		if unrecovered > 0 {
			return nil
		}

		return tx.Model(&customModels.CustomIncident{}).
			Where("id = ? AND status = ?", incidentId, customModels.IncidentStatusOpen).
			Updates(map[string]interface{}{
				"status":      customModels.IncidentStatusResolved,
				"resolved_at": now,
			}).Error
	})
}

// MarkStormFired flips the storm flag on an incident, used by the storm-
// detection branch to ensure we only emit one storm notification per incident.
// Returns true iff the flag transitioned 0→1 (caller should fire its
// notification only on a true return).
func (r *Repo) MarkStormFired(incidentId int64) (bool, error) {
	res := models.DB(r.ctx).Model(&customModels.CustomIncident{}).
		Where("id = ? AND storm_fired = 0", incidentId).
		Update("storm_fired", 1)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// EncodeDimensionValues serializes a dimension-value map for storage in
// CustomIncident.DimensionValues. Errors are unlikely (map[string]string
// always marshals) but bubbled up rather than silently swallowed.
func EncodeDimensionValues(values map[string]string) (string, error) {
	b, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
