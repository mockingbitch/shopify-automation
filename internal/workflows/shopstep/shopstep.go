// Package shopstep holds helpers shared by workflows that call Shopee for
// one shop: access with a forced refresh on a rejected token, and mapping
// errors to step outcomes.
package shopstep

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/shopee"
	"shopee-automation/internal/shops"
	"shopee-automation/internal/workflow"
)

// WithAccess runs fn with a valid access token. If Shopee rejects the token
// (it may have been invalidated early), the pair is refreshed once and fn is
// retried; a rejected refresh marks the shop RECONNECT_REQUIRED.
func WithAccess(ctx context.Context, s *shops.Service, orgID, shopID uuid.UUID, fn func(acc shops.Access) error) error {
	acc, err := s.AccessFor(ctx, orgID, shopID)
	if err != nil {
		return err
	}
	err = fn(acc)
	if !shopee.IsKind(err, shopee.KindAuth) {
		return err
	}
	// Force a refresh: every access token is shorter-lived than this.
	if rerr := s.Refresh(ctx, orgID, shopID, shopee.AccessTokenLifetime+time.Hour); rerr != nil {
		return rerr
	}
	if acc, err = s.AccessFor(ctx, orgID, shopID); err != nil {
		return err
	}
	return fn(acc)
}

// FromErr turns an error into the right step outcome.
func FromErr(err error) workflow.Result {
	if ae, ok := apperr.As(err); ok {
		switch ae.Kind {
		case apperr.KindFailedDependency:
			return workflow.NeedsUser("reconnect_required", ae.Message)
		case apperr.KindNotFound, apperr.KindInvalid:
			return workflow.Permanent(errors.New(ae.Message))
		}
		if se, ok := shopee.AsError(err); ok {
			return FromShopee(se)
		}
		return workflow.Retryable(errors.New(ae.Message))
	}
	if se, ok := shopee.AsError(err); ok {
		return FromShopee(se)
	}
	return workflow.Retryable(err)
}

// FromShopee maps a normalized Shopee error.
func FromShopee(e *shopee.Error) workflow.Result {
	err := fmt.Errorf("lỗi từ Shopee %s: %s (mã yêu cầu %s)", e.Code, e.Message, e.RequestID)
	switch e.Kind {
	case shopee.KindTransient, shopee.KindRateLimited, shopee.KindAuth:
		return workflow.Retryable(err)
	case shopee.KindAuthorizationLost:
		return workflow.NeedsUser("reconnect_required", "Shopee không còn chấp nhận ủy quyền của shop này. Hãy kết nối lại shop rồi bấm Tiếp tục.")
	case shopee.KindAmbiguous:
		return workflow.Ambiguous(err)
	default:
		return workflow.Permanent(err)
	}
}

// ValidateConnection is a step that checks the shop can be called.
func ValidateConnection(s *shops.Service) workflow.StepHandler {
	return func(ctx context.Context, sc *workflow.StepContext) workflow.Result {
		var info shopee.ShopInfo
		var acc shops.Access
		err := WithAccess(ctx, s, sc.OrgID, sc.ShopID, func(a shops.Access) error {
			var err error
			acc = a
			info, err = s.Shopee.GetShopInfo(ctx, a.App, a.AccessToken, a.ShopID)
			return err
		})
		if err != nil {
			return FromErr(err)
		}
		if info.Status != "" && info.Status != "NORMAL" {
			return workflow.NeedsUser("shop_not_normal", "Shopee báo shop đang ở trạng thái "+info.Status+". Hãy xử lý trong Kênh Người Bán rồi bấm Tiếp tục.")
		}
		return workflow.Succeeded(map[string]any{"shopee_shop_id": fmt.Sprint(acc.ShopID), "region": info.Region, "status": info.Status})
	}
}
