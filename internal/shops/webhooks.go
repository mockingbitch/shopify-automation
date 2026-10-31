package shops

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/queue"
	"shopee-automation/internal/shopee"
	"shopee-automation/internal/shops/shopsdb"
)

// Push codes handled in v1 (developer guide 18 and the push reference pages
// shop_authorization_push, shop_authorization_canceled_push and
// open_api_authorization_expiry, read 2026-10-30).
const (
	PushAuthorized      = 1
	PushAuthCanceled    = 2
	PushAuthExpiresSoon = 12
)

// MaxWebhookBody bounds push bodies (authorization pushes are small).
const MaxWebhookBody = 64 << 10

var errBadSignature = apperr.Unauthenticated("Chữ ký push không hợp lệ.")

// pushBody is the subset of a push we read. Field names follow the
// documented samples; code 2 samples use both "shop_id" and "shopid".
type pushBody struct {
	Code      int   `json:"code"`
	PartnerID int64 `json:"partner_id"`
	Timestamp int64 `json:"timestamp"`
	ShopID    int64 `json:"shop_id"`
	Data      struct {
		AuthorizeType string  `json:"authorize_type"`
		ShopID        int64   `json:"shop_id"`
		ShopIDAlt     int64   `json:"shopid"`
		ShopIDList    []int64 `json:"shop_id_list"`
		ShopExpire    []int64 `json:"shop_expire_soon"`
		ExpireBefore  int64   `json:"expire_before"`
		MainAccountID int64   `json:"main_account_id"`
	} `json:"data"`
}

func (b pushBody) shopIDs() []int64 {
	seen := map[int64]bool{}
	var out []int64
	add := func(ids ...int64) {
		for _, id := range ids {
			if id > 0 && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	add(b.ShopID, b.Data.ShopID, b.Data.ShopIDAlt)
	add(b.Data.ShopIDList...)
	add(b.Data.ShopExpire...)
	if out == nil {
		out = []int64{}
	}
	return out
}

// ReceiveWebhook verifies and stores one push. It must answer fast: the
// processing happens on the worker. Duplicate deliveries are acknowledged
// and dropped.
func (s *Service) ReceiveWebhook(ctx context.Context, appID uuid.UUID, body []byte, authorization string) error {
	app, err := s.loadApp(ctx, appID)
	if err != nil {
		return wrap(err, apperr.NotFound("Không tìm thấy đường dẫn này."))
	}
	if app.PushCallbackUrl == nil {
		return errBadSignature
	}
	capp, err := s.clientApp(app)
	if err != nil {
		return apperr.Internal(err)
	}
	if !shopee.VerifyPush(capp.PartnerKey, *app.PushCallbackUrl, body, authorization) {
		s.Log.WarnContext(ctx, "webhook: bad signature", slog.String("shopee_app_id", appID.String()))
		return errBadSignature
	}
	var pb pushBody
	if err := json.Unmarshal(body, &pb); err != nil {
		return apperr.BadRequest("bad_request", "Dữ liệu gửi lên không phải JSON hợp lệ.")
	}
	if pb.PartnerID != 0 && pb.PartnerID != app.PartnerID {
		return errBadSignature
	}
	sum := sha256.Sum256(body)
	var eventID uuid.UUID
	err = s.tx(ctx, db.Scope{}, func(q *shopsdb.Queries, _ pgx.Tx) error {
		id, err := q.InsertWebhookEvent(ctx, shopsdb.InsertWebhookEventParams{ID: db.NewID(), ShopeeAppID: appID,
			PushCode: int32(pb.Code), ShopeeShopIds: pb.shopIDs(), BodySha256: sum[:], Body: body, ReceivedAt: s.now()}) // #nosec G115 -- push codes are small
		if db.IsNoRows(err) {
			return nil // duplicate delivery
		}
		eventID = id
		return err
	})
	if err != nil {
		return wrap(err, nil)
	}
	if eventID != uuid.Nil {
		if _, err := s.Queue.Enqueue(ctx, queue.TypeWebhookProcess, WebhookPayload{EventID: eventID},
			queue.Options{ID: "webhook:" + eventID.String(), MaxRetry: 10, Timeout: time.Minute}); err != nil {
			// Stored already; the sweeper picks it up.
			s.Log.WarnContext(ctx, "webhook: enqueue failed", slog.String("error", err.Error()))
		}
	}
	return nil
}

// WebhookPayload is the task payload for processing one push.
type WebhookPayload struct {
	EventID uuid.UUID `json:"event_id"`
}

// HandleWebhook is the worker handler for queue.TypeWebhookProcess.
func (s *Service) HandleWebhook(ctx context.Context, payload []byte) error {
	var p WebhookPayload
	if err := queue.Decode(payload, &p); err != nil {
		return err
	}
	return s.ProcessWebhook(ctx, p.EventID)
}

// ProcessWebhook applies one stored push. It is idempotent: only events in
// RECEIVED are processed.
func (s *Service) ProcessWebhook(ctx context.Context, eventID uuid.UUID) error {
	if s.System == nil {
		return errors.New("shops: system database is not configured")
	}
	var procErr error
	err := s.tx(ctx, db.Scope{}, func(q *shopsdb.Queries, _ pgx.Tx) error {
		ev, err := q.GetWebhookEventForUpdate(ctx, eventID)
		if err != nil {
			if db.IsNoRows(err) {
				return nil
			}
			return err
		}
		if ev.Status != "RECEIVED" {
			return nil
		}
		status, perr := s.applyPush(ctx, ev)
		var lastErr *string
		if perr != nil {
			procErr = perr
			msg := perr.Error()
			lastErr = &msg
			status = "RECEIVED" // retried by the queue and the sweeper
			if ev.Attempts >= 9 {
				status = "FAILED"
			}
		}
		return q.FinishWebhookEvent(ctx, shopsdb.FinishWebhookEventParams{Status: status, LastError: lastErr, Now: s.now(), ID: ev.ID})
	})
	if err != nil {
		return err
	}
	// The attempt is recorded; report the failure so the queue retries.
	return procErr
}

func (s *Service) applyPush(ctx context.Context, ev shopsdb.WebhookEvent) (string, error) {
	var pb pushBody
	if err := json.Unmarshal(ev.Body, &pb); err != nil {
		return "IGNORED", nil
	}
	switch pb.Code {
	case PushAuthorized:
		// Tokens only arrive through our OAuth callback; nothing to change.
		return "PROCESSED", nil
	case PushAuthCanceled, PushAuthExpiresSoon:
	default:
		return "IGNORED", nil
	}
	ids := pb.shopIDs()
	if len(ids) == 0 {
		return "PROCESSED", nil // merchant-level push; merchants are not tracked in v1
	}
	var conns []shopsdb.SystemFindConnectionsRow
	err := s.System.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
		var err error
		conns, err = shopsdb.New(tx).SystemFindConnections(ctx, shopsdb.SystemFindConnectionsParams{ShopeeAppID: ev.ShopeeAppID, ShopIds: ids})
		return err
	})
	if err != nil {
		return "", err
	}
	for _, c := range conns {
		if err := s.applyToConnection(ctx, c, pb, ev.ID); err != nil {
			return "", err
		}
	}
	return "PROCESSED", nil
}

func (s *Service) applyToConnection(ctx context.Context, c shopsdb.SystemFindConnectionsRow, pb pushBody, eventID uuid.UUID) error {
	meta := map[string]any{"webhook_event_id": eventID.String(), "push_code": pb.Code, "shopee_shop_id": c.ShopeeShopID}
	return s.tx(ctx, db.Scope{OrgID: c.OrgID}, func(q *shopsdb.Queries, tx pgx.Tx) error {
		conn, err := q.LockConnection(ctx, shopsdb.LockConnectionParams{OrgID: c.OrgID, ID: c.ID})
		if err != nil {
			return err
		}
		switch pb.Code {
		case PushAuthCanceled:
			if conn.Status == StatusRevoked || conn.Status == StatusDisconnected {
				return nil
			}
			status, event, action := StatusRevoked, "REVOKED", "shop.authorization_revoked"
			if pb.Data.AuthorizeType == "expiry" {
				status, event, action = StatusExpired, "EXPIRED", "shop.authorization_expired"
			}
			meta["authorize_type"] = pb.Data.AuthorizeType
			if err := q.DeleteCredentials(ctx, shopsdb.DeleteCredentialsParams{OrgID: c.OrgID, ShopConnectionID: c.ID}); err != nil {
				return err
			}
			if _, err := q.SetConnectionStatus(ctx, shopsdb.SetConnectionStatusParams{Status: status, Now: s.now(), OrgID: c.OrgID, ID: c.ID}); err != nil {
				return err
			}
			if err := s.recordAuthorization(ctx, q, c.OrgID, c.ID, event, "push", meta); err != nil {
				return err
			}
			if err := s.notifyReconnect(ctx, q, conn, "Ủy quyền của shop trên Shopee đã kết thúc. Hãy kết nối lại shop để quy trình tự động tiếp tục chạy."); err != nil {
				return err
			}
			return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &c.OrgID, ActorType: audit.ActorShopee, Action: action,
				TargetType: "shop_connection", TargetID: &c.ID, Metadata: meta})
		case PushAuthExpiresSoon:
			var before *time.Time
			if pb.Data.ExpireBefore > 0 {
				before = ptr(time.Unix(pb.Data.ExpireBefore, 0).UTC())
				if conn.AuthorizationExpiresAt == nil || conn.AuthorizationExpiresAt.After(*before) {
					if err := q.SetAuthorizationExpiry(ctx, shopsdb.SetAuthorizationExpiryParams{ExpiresAt: before, OrgID: c.OrgID, ID: c.ID}); err != nil {
						return err
					}
				}
			}
			if err := s.recordAuthorizationAt(ctx, q, c.OrgID, c.ID, "EXPIRING", "push", nil, before, meta); err != nil {
				return err
			}
			return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &c.OrgID, ActorType: audit.ActorShopee, Action: "shop.authorization_expiring",
				TargetType: "shop_connection", TargetID: &c.ID, Metadata: meta})
		}
		return nil
	})
}

// SweepWebhooks re-enqueues events that are still RECEIVED after a minute
// (for example when Redis was down at receive time).
func (s *Service) SweepWebhooks(ctx context.Context) (int, error) {
	var ids []uuid.UUID
	err := s.tx(ctx, db.Scope{}, func(q *shopsdb.Queries, _ pgx.Tx) error {
		var err error
		ids, err = q.ListPendingWebhookEvents(ctx, shopsdb.ListPendingWebhookEventsParams{Before: s.now().Add(-time.Minute), Lim: 200})
		return err
	})
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if _, err := s.Queue.Enqueue(ctx, queue.TypeWebhookProcess, WebhookPayload{EventID: id},
			queue.Options{ID: "webhook:" + id.String() + ":" + s.now().Format("200601021504"), MaxRetry: 3, Timeout: time.Minute}); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}
