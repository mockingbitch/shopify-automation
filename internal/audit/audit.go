// Package audit records an append-only trail of security-relevant and
// state-changing actions. Entries are written in the same transaction as the
// change they describe, so a change without its audit row cannot commit.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit/auditdb"
	"shopee-automation/internal/platform/clock"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/logx"
	"shopee-automation/internal/platform/reqmeta"
)

// Outcome values.
const (
	Success = "success"
	Failure = "failure"
	Denied  = "denied"
)

// Actor types.
const (
	ActorUser   = "user"
	ActorSystem = "system"
	ActorShopee = "shopee"
)

// Entry describes one audited action.
type Entry struct {
	OrgID       *uuid.UUID // nil for user-level events (login, password reset)
	ActorUserID *uuid.UUID
	ActorType   string // defaults to "user" when ActorUserID is set, else "system"
	Action      string // dotted verb, e.g. "member.role_changed"
	TargetType  string
	TargetID    *uuid.UUID
	Outcome     string // defaults to Success
	Metadata    map[string]any
}

// Recorder writes audit entries.
type Recorder struct {
	Clock clock.Clock
}

// Record inserts e using tx. Request metadata (request ID, IP, user agent) is
// taken from ctx. Metadata is redacted before storage.
func (r Recorder) Record(ctx context.Context, tx pgx.Tx, e Entry) error {
	if e.Action == "" {
		return fmt.Errorf("audit: action is required")
	}
	if e.Outcome == "" {
		e.Outcome = Success
	}
	if e.ActorType == "" {
		e.ActorType = ActorSystem
		if e.ActorUserID != nil {
			e.ActorType = ActorUser
		}
	}
	meta, err := json.Marshal(nonNil(logx.RedactMap(e.Metadata)))
	if err != nil {
		return fmt.Errorf("audit: marshal metadata: %w", err)
	}
	m := reqmeta.From(ctx)
	var target *string
	if e.TargetType != "" {
		target = &e.TargetType
	}
	var reqID, ua *string
	if m.RequestID != "" {
		reqID = &m.RequestID
	}
	if m.UserAgent != "" {
		ua = &m.UserAgent
	}
	now := time.Now().UTC()
	if r.Clock != nil {
		now = r.Clock.Now()
	}
	return auditdb.New(tx).InsertAuditLog(ctx, auditdb.InsertAuditLogParams{
		ID:          db.NewID(),
		OrgID:       e.OrgID,
		ActorUserID: e.ActorUserID,
		ActorType:   e.ActorType,
		Action:      e.Action,
		TargetType:  target,
		TargetID:    e.TargetID,
		Outcome:     e.Outcome,
		Metadata:    meta,
		Ip:          m.IP,
		UserAgent:   ua,
		RequestID:   reqID,
		CreatedAt:   now,
	})
}

func nonNil(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// Ptr returns a pointer to id, for building entries.
func Ptr(id uuid.UUID) *uuid.UUID { return &id }
