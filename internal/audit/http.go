package audit

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit/auditdb"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/httpx"
)

// Handler serves the audit log API.
type Handler struct {
	DB *db.DB
}

// LogView is the API representation of an audit entry.
type LogView struct {
	ID          uuid.UUID       `json:"id"`
	ActorUserID *uuid.UUID      `json:"actor_user_id"`
	ActorType   string          `json:"actor_type"`
	Action      string          `json:"action"`
	TargetType  *string         `json:"target_type"`
	TargetID    *uuid.UUID      `json:"target_id"`
	Outcome     string          `json:"outcome"`
	Metadata    json.RawMessage `json:"metadata"`
	IP          *string         `json:"ip"`
	RequestID   *string         `json:"request_id"`
	CreatedAt   time.Time       `json:"created_at"`
}

type cursor struct {
	CreatedAt time.Time `json:"t"`
	ID        uuid.UUID `json:"i"`
}

// List handles GET /orgs/{org_id}/audit-logs.
func (h Handler) List(w http.ResponseWriter, r *http.Request) error {
	t := authctx.MustTenant(r.Context())
	q := r.URL.Query()
	limit, err := httpx.QueryInt(r, "limit", 50, 1, 200)
	if err != nil {
		return err
	}
	p := auditdb.ListAuditLogsParams{OrgID: &t.OrgID, RowLimit: int32(limit) + 1} //nolint:gosec // limit is validated to 1..200
	var fields []apperr.FieldError
	if v := q.Get("actor_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			fields = append(fields, apperr.FieldError{Field: "actor_id", Code: "invalid_format", Message: "Must be a UUID."})
		}
		p.ActorUserID = &id
	}
	if v := q.Get("action"); v != "" {
		p.Action = &v
	}
	if v := q.Get("target_type"); v != "" {
		p.TargetType = &v
	}
	for _, k := range []string{"from", "to"} {
		if v := q.Get(k); v != "" {
			ts, err := time.Parse(time.RFC3339, v)
			if err != nil {
				fields = append(fields, apperr.FieldError{Field: k, Code: "invalid_format", Message: "Must be an RFC 3339 timestamp."})
				continue
			}
			if k == "from" {
				p.FromTime = &ts
			} else {
				p.ToTime = &ts
			}
		}
	}
	if v := q.Get("cursor"); v != "" {
		c, ok := decodeCursor(v)
		if !ok {
			fields = append(fields, apperr.FieldError{Field: "cursor", Code: "invalid_format", Message: "Cursor is invalid."})
		} else {
			p.CursorCreatedAt, p.CursorID = &c.CreatedAt, &c.ID
		}
	}
	if len(fields) > 0 {
		return apperr.Invalid(fields...)
	}

	ctx := r.Context()
	var rows []auditdb.AuditLog
	err = h.DB.Tx(ctx, db.Scope{OrgID: t.OrgID}, func(tx pgx.Tx) error {
		var err error
		rows, err = auditdb.New(tx).ListAuditLogs(ctx, p)
		return err
	})
	if err != nil {
		return err
	}
	out := httpx.List[LogView]{Data: make([]LogView, 0, len(rows))}
	if len(rows) > limit {
		last := rows[limit-1]
		s := encodeCursor(cursor{CreatedAt: last.CreatedAt, ID: last.ID})
		out.NextCursor = &s
		rows = rows[:limit]
	}
	for _, row := range rows {
		var ip *string
		if row.Ip != nil {
			s := row.Ip.String()
			ip = &s
		}
		out.Data = append(out.Data, LogView{
			ID: row.ID, ActorUserID: row.ActorUserID, ActorType: row.ActorType, Action: row.Action,
			TargetType: row.TargetType, TargetID: row.TargetID, Outcome: row.Outcome,
			Metadata: json.RawMessage(row.Metadata), IP: ip, RequestID: row.RequestID, CreatedAt: row.CreatedAt,
		})
	}
	return httpx.JSON(w, http.StatusOK, out)
}

func encodeCursor(c cursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (cursor, bool) {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return cursor{}, false
	}
	var c cursor
	if err := json.Unmarshal(b, &c); err != nil || c.ID == uuid.Nil {
		return cursor{}, false
	}
	return c, true
}
