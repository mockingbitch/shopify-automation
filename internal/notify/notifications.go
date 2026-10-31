package notify

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/notify/notifydb"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/clock"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/httpx"
)

// Service reads and marks the caller's notifications in one organization.
// Notifications are written by the modules that raise them, in their own
// transactions (workflow engine, shops).
type Service struct {
	DB    *db.DB
	Clock clock.Clock
}

// Notification is one in-app notification.
type Notification struct {
	ID        uuid.UUID  `json:"id"`
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Link      string     `json:"link"`
	CreatedAt time.Time  `json:"created_at"`
	ReadAt    *time.Time `json:"read_at"`
}

// List is a page of notifications plus the unread count.
type List struct {
	Data   []Notification `json:"data"`
	Unread int64          `json:"unread"`
}

func (s *Service) tx(ctx context.Context, p authctx.Principal, t authctx.Tenant, fn func(q *notifydb.Queries) error) error {
	return s.DB.Tx(ctx, db.Scope{OrgID: t.OrgID, UserID: p.UserID}, func(tx pgx.Tx) error { return fn(notifydb.New(tx)) })
}

// List returns the newest notifications of the caller.
func (s *Service) List(ctx context.Context, p authctx.Principal, t authctx.Tenant, unreadOnly bool, limit int) (List, error) {
	out := List{Data: []Notification{}}
	err := s.tx(ctx, p, t, func(q *notifydb.Queries) error {
		rows, err := q.ListNotifications(ctx, notifydb.ListNotificationsParams{OrgID: t.OrgID, UserID: p.UserID, UnreadOnly: unreadOnly, Lim: int32(limit)}) // #nosec G115 -- limit ≤ 100
		if err != nil {
			return err
		}
		for _, r := range rows {
			out.Data = append(out.Data, Notification{ID: r.ID, Kind: r.Kind, Title: r.Title, Body: r.Body, Link: r.Link,
				CreatedAt: r.CreatedAt.UTC(), ReadAt: r.ReadAt})
		}
		out.Unread, err = q.CountUnread(ctx, notifydb.CountUnreadParams{OrgID: t.OrgID, UserID: p.UserID})
		return err
	})
	if err != nil {
		return List{}, apperr.Internal(err)
	}
	return out, nil
}

// MarkRead marks one notification (id) or all of them (id == uuid.Nil).
func (s *Service) MarkRead(ctx context.Context, p authctx.Principal, t authctx.Tenant, id uuid.UUID) error {
	now := s.Clock.Now()
	err := s.tx(ctx, p, t, func(q *notifydb.Queries) error {
		if id == uuid.Nil {
			return q.MarkAllRead(ctx, notifydb.MarkAllReadParams{Now: &now, OrgID: t.OrgID, UserID: p.UserID})
		}
		n, err := q.MarkRead(ctx, notifydb.MarkReadParams{Now: &now, OrgID: t.OrgID, UserID: p.UserID, ID: id})
		if err == nil && n == 0 {
			return apperr.NotFound("Không tìm thấy thông báo.")
		}
		return err
	})
	if _, ok := apperr.As(err); ok || err == nil {
		return err
	}
	return apperr.Internal(err)
}

// Handler serves the notification endpoints.
type Handler struct{ Svc *Service }

// List handles GET /orgs/{orgID}/notifications.
func (h Handler) List(w http.ResponseWriter, r *http.Request) error {
	limit := 30
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			return apperr.Invalid(apperr.FieldError{Field: "limit", Code: "invalid", Message: "Phải từ 1 đến 100."})
		}
		limit = n
	}
	out, err := h.Svc.List(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), r.URL.Query().Get("unread") == "true", limit)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, out)
}

// Read handles POST /orgs/{orgID}/notifications/{notificationID}/read.
func (h Handler) Read(w http.ResponseWriter, r *http.Request) error {
	id, err := uuid.Parse(chi.URLParam(r, "notificationID"))
	if err != nil || id == uuid.Nil {
		return apperr.NotFound("Không tìm thấy thông báo.")
	}
	if err := h.Svc.MarkRead(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), id); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

// ReadAll handles POST /orgs/{orgID}/notifications/read-all.
func (h Handler) ReadAll(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.MarkRead(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), uuid.Nil); err != nil {
		return err
	}
	return httpx.NoContent(w)
}
