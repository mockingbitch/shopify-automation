// Package notify holds in-app notifications, the email outbox and the
// Shopee catalog cache used by the dashboard pickers.
package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/notify/notifydb"
	"shopee-automation/internal/platform/clock"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/envelope"
	"shopee-automation/internal/platform/mailer"
	"shopee-automation/internal/platform/queue"
)

const (
	outboxTable     = "email_outbox"
	maxMailAttempts = 8
)

// Outbox is a mailer.Mailer that stores each message encrypted and sends
// it from a retried background task, so a mail server outage does not lose
// verification or invitation emails.
type Outbox struct {
	DB     *db.DB
	System *db.DB // sweeper only
	Keys   *envelope.Keyring
	Queue  queue.Enqueuer
	// Mailer delivers the message (SMTP in production).
	Mailer mailer.Mailer
	Clock  clock.Clock
	Log    *slog.Logger
}

// MailPayload is the queue payload: the outbox row ID only.
type MailPayload struct {
	ID uuid.UUID `json:"id"`
}

// Send stores the message and queues delivery.
func (o *Outbox) Send(ctx context.Context, m mailer.Message) error {
	id := db.NewID()
	row, wrapped, err := o.Keys.NewRow(outboxTable, id.String())
	if err != nil {
		return err
	}
	plain, _ := json.Marshal(m)
	sealed, err := row.Seal("message_enc", plain)
	if err != nil {
		return err
	}
	now := o.Clock.Now()
	if err := o.DB.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
		return notifydb.New(tx).InsertEmail(ctx, notifydb.InsertEmailParams{ID: id, KekID: wrapped.KEKID, DekWrapped: wrapped.DEK,
			MessageEnc: sealed, CreatedAt: now})
	}); err != nil {
		return fmt.Errorf("outbox: store email: %w", err)
	}
	if _, err := o.Queue.Enqueue(ctx, queue.TypeMailSend, MailPayload{ID: id}, queue.Options{MaxRetry: maxMailAttempts, Timeout: time.Minute}); err != nil {
		// The sweeper sends it later.
		o.Log.WarnContext(ctx, "outbox: enqueue failed", slog.String("error", err.Error()))
	}
	return nil
}

// HandleSend delivers one stored email and deletes it.
func (o *Outbox) HandleSend(ctx context.Context, payload []byte) error {
	var p MailPayload
	if err := queue.Decode(payload, &p); err != nil {
		return err
	}
	var sendErr error
	err := o.DB.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
		q := notifydb.New(tx)
		row, err := q.LockEmail(ctx, p.ID)
		if db.IsNoRows(err) {
			return nil // sent already, failed for good, or another worker has it
		}
		if err != nil {
			return err
		}
		r, err := o.Keys.OpenRow(outboxTable, row.ID.String(), envelope.WrappedKey{KEKID: row.KekID, DEK: row.DekWrapped})
		if err != nil {
			return err
		}
		plain, err := r.Open("message_enc", row.MessageEnc)
		if err != nil {
			return err
		}
		var m mailer.Message
		if err := json.Unmarshal(plain, &m); err != nil {
			return err
		}
		if sendErr = o.Mailer.Send(ctx, m); sendErr != nil {
			now := o.Clock.Now()
			msg := sendErr.Error()
			if len(msg) > 500 {
				msg = msg[:500]
			}
			backoff := time.Duration(1<<min(row.Attempts, 10)) * 30 * time.Second
			if err := q.EmailFailed(ctx, notifydb.EmailFailedParams{LastError: &msg, NextAttemptAt: now.Add(backoff), MaxAttempts: maxMailAttempts,
				Now: now, ID: row.ID}); err != nil {
				return err
			}
			o.Log.WarnContext(ctx, "outbox: send failed", slog.String("email_id", row.ID.String()), slog.Int("attempt", int(row.Attempts)+1))
			return nil // keep the failure record
		}
		return q.DeleteEmail(ctx, row.ID)
	})
	if err != nil {
		return err
	}
	if sendErr != nil {
		return fmt.Errorf("outbox: send: %w", sendErr) // the queue retries
	}
	return nil
}

// Sweep re-queues emails whose delivery task was lost.
func (o *Outbox) Sweep(ctx context.Context) (int, error) {
	if o.System == nil {
		return 0, errors.New("outbox: system database is not configured")
	}
	var rows []notifydb.SystemDueEmailsRow
	err := o.System.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
		var err error
		rows, err = notifydb.New(tx).SystemDueEmails(ctx, notifydb.SystemDueEmailsParams{Before: o.Clock.Now().Add(-time.Minute), Lim: 200})
		return err
	})
	if err != nil {
		return 0, err
	}
	for _, r := range rows {
		id := fmt.Sprintf("mail:%s:%d", r.ID, r.Attempts)
		if _, err := o.Queue.Enqueue(ctx, queue.TypeMailSend, MailPayload{ID: r.ID}, queue.Options{ID: id, NoRetry: true, Timeout: time.Minute}); err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}
