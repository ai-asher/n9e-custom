package router

import (
	"errors"
	"time"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"

	"github.com/gin-gonic/gin"
	"github.com/toolkits/pkg/ginx"
	"gorm.io/gorm"
)

const (
	// EmergencyMuteSingletonID is the only row id we allow. The table is
	// modeled as a singleton because the global mute is a binary toggle —
	// multi-row scope is intentionally not supported.
	EmergencyMuteSingletonID = 1

	// emergencyMuteMaxDurationSec is the safety cap on expire_at. 24 hours
	// is generous for a planned maintenance window and short enough to
	// guard against "I'll just leave this on for now" forever-mute scenarios.
	emergencyMuteMaxDurationSec = 24 * 3600
)

func (r *Router) emergencyMuteGet(c *gin.Context) {
	row, err := getEmergencyMute(r)
	if err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}
	if row == nil {
		// No row yet → return a synthesized "disabled" state so the front
		// end always sees a stable shape.
		ginx.NewRender(c).Data(&customModels.CustomEmergencyMute{
			Id:      EmergencyMuteSingletonID,
			Enabled: 0,
		}, nil)
		return
	}
	hydrateEmergencyMute(row)
	ginx.NewRender(c).Data(row, nil)
}

// emergencyMutePutReq is the only write shape — there is no separate Create
// endpoint because the table is a singleton. PUT both creates (if absent)
// and updates (if present) the id=1 row.
type emergencyMutePutReq struct {
	Enabled       int     `json:"enabled"`        // 0 or 1
	Reason        string  `json:"reason"`         // required when enabling
	ExpireAt      int64   `json:"expire_at"`      // required when enabling, must be in the future
	DatasourceIds []int64 `json:"datasource_ids"` // optional scope
	GroupIds      []int64 `json:"group_ids"`      // optional scope
}

// validate enforces the safety rules we agreed on:
//   - Enabling requires a non-empty reason.
//   - Enabling requires expire_at in the future.
//   - expire_at - now must not exceed emergencyMuteMaxDurationSec.
//
// Disabling (Enabled=0) sidesteps every check — there is no scenario where
// blocking a turn-off is the right call.
func (req *emergencyMutePutReq) validate(now int64) error {
	if req.Enabled == 0 {
		return nil
	}
	if req.Reason == "" {
		return ginxBadRequest("reason is required when enabling emergency mute")
	}
	if req.ExpireAt <= now {
		return ginxBadRequest("expire_at must be in the future (got %d, now=%d)", req.ExpireAt, now)
	}
	if req.ExpireAt-now > emergencyMuteMaxDurationSec {
		return ginxBadRequest("expire_at exceeds 24-hour maximum (delta=%ds)", req.ExpireAt-now)
	}
	return nil
}

func (r *Router) emergencyMutePut(c *gin.Context) {
	var req emergencyMutePutReq
	ginx.BindJSON(c, &req)

	now := time.Now().Unix()
	if err := req.validate(now); err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}

	existing, err := getEmergencyMute(r)
	if err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}

	row := &customModels.CustomEmergencyMute{
		Id:                EmergencyMuteSingletonID,
		Enabled:           req.Enabled,
		Reason:            req.Reason,
		ExpireAt:          req.ExpireAt,
		DatasourceIds:     encodeJSONList(req.DatasourceIds),
		DatasourceIdsJson: req.DatasourceIds,
		GroupIds:          encodeJSONList(req.GroupIds),
		GroupIdsJson:      req.GroupIds,
		UpdateBy:          currentUsername(c),
		UpdateAt:          now,
	}
	if existing == nil {
		// First write — populate create_*.
		row.CreateBy = row.UpdateBy
		row.CreateAt = now
		if err := models.DB(r.Ctx).Create(row).Error; err != nil {
			ginx.NewRender(c).Data(nil, err)
			return
		}
	} else {
		row.CreateBy = existing.CreateBy
		row.CreateAt = existing.CreateAt
		if err := models.DB(r.Ctx).Save(row).Error; err != nil {
			ginx.NewRender(c).Data(nil, err)
			return
		}
	}

	r.auditWrite.Write(c, "put", "emergency_mute", existing, row)
	hydrateEmergencyMute(row)
	ginx.NewRender(c).Data(row, nil)
}

// getEmergencyMute returns the singleton row or nil if it doesn't exist.
// We treat ErrRecordNotFound as a non-error to keep the callers simple.
func getEmergencyMute(r *Router) (*customModels.CustomEmergencyMute, error) {
	var row customModels.CustomEmergencyMute
	err := models.DB(r.Ctx).Where("id = ?", EmergencyMuteSingletonID).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func hydrateEmergencyMute(row *customModels.CustomEmergencyMute) {
	row.DatasourceIdsJson = decodeJSONInt64List(row.DatasourceIds)
	row.GroupIdsJson = decodeJSONInt64List(row.GroupIds)
}
