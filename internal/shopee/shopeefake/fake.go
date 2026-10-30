// Package shopeefake is an in-process test double of the Shopee Open
// Platform endpoints this project uses. It is for tests only and is never
// linked into a binary. It follows the documented contract: it verifies
// signatures and timestamps, treats authorization codes and refresh tokens as
// single-use, and returns the documented response shapes and error codes.
package shopeefake

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"time"

	"shopee-automation/internal/shopee/signer"
)

// Shop is a fake seller shop.
type Shop struct {
	ID          int64
	Name        string
	Region      string
	Status      string
	Logo        string
	Description string
	ExpireTime  time.Time
}

type token struct {
	shopIDs   []int64
	expiresAt time.Time
}

// Server is the fake. Lock-protected fields may be changed by tests.
type Server struct {
	*httptest.Server
	PartnerID  int64
	PartnerKey []byte
	Now        func() time.Time

	mu           sync.Mutex
	shops        map[int64]*Shop
	codes        map[string]codeGrant
	access       map[string]token
	refresh      map[string][]int64 // refresh token -> shops still allowed to use it
	calls        map[string]int
	failNext     map[string]failure
	RefreshDelay time.Duration
}

type codeGrant struct {
	shopID        int64
	mainAccountID int64
	shopIDs       []int64
	expiresAt     time.Time
}

type failure struct {
	status  int
	code    string
	message string
	hang    bool
}

// New starts a fake with the given partner credentials.
func New(partnerID int64, partnerKey []byte) *Server {
	s := &Server{
		PartnerID: partnerID, PartnerKey: partnerKey, Now: time.Now,
		shops: map[int64]*Shop{}, codes: map[string]codeGrant{}, access: map[string]token{},
		refresh: map[string][]int64{}, calls: map[string]int{}, failNext: map[string]failure{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v2/auth/token/get", s.tokenGet)
	mux.HandleFunc("POST /api/v2/auth/access_token/get", s.refreshGet)
	mux.HandleFunc("GET /api/v2/shop/get_shop_info", s.shopInfo)
	mux.HandleFunc("GET /api/v2/shop/get_profile", s.profile)
	s.Server = httptest.NewServer(mux)
	return s
}

// AddShop registers a shop.
func (s *Server) AddShop(sh Shop) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sh.Status == "" {
		sh.Status = "NORMAL"
	}
	if sh.ExpireTime.IsZero() {
		sh.ExpireTime = s.Now().Add(365 * 24 * time.Hour)
	}
	s.shops[sh.ID] = &sh
}

// Authorize simulates a seller authorizing one shop; it returns the code
// Shopee would append to the redirect URL.
func (s *Server) Authorize(shopID int64) string {
	return s.grant(codeGrant{shopID: shopID})
}

// AuthorizeMain simulates a main-account authorization covering shopIDs.
func (s *Server) AuthorizeMain(mainAccountID int64, shopIDs ...int64) string {
	return s.grant(codeGrant{mainAccountID: mainAccountID, shopIDs: shopIDs})
}

func (s *Server) grant(g codeGrant) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	g.expiresAt = s.Now().Add(10 * time.Minute)
	c := random()
	s.codes[c] = g
	return c
}

// FailNext makes the next call to path fail with a Shopee error body.
func (s *Server) FailNext(path string, status int, code, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failNext[path] = failure{status: status, code: code, message: message}
}

// HangNext makes the next call to path never answer (until the client gives
// up), after the request was received.
func (s *Server) HangNext(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failNext[path] = failure{hang: true}
}

// Calls returns how many requests reached path (including failures).
func (s *Server) Calls(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[path]
}

// RevokeTokens invalidates every access and refresh token of shopID.
func (s *Server) RevokeTokens(shopID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, t := range s.access {
		for _, id := range t.shopIDs {
			if id == shopID {
				delete(s.access, k)
			}
		}
	}
	for k, ids := range s.refresh {
		s.refresh[k] = remove(ids, shopID)
	}
}

// ExpireAccessTokens makes every current access token invalid.
func (s *Server) ExpireAccessTokens() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.access = map[string]token{}
}

func (s *Server) begin(w http.ResponseWriter, r *http.Request, kind signer.Kind) (body map[string]any, shopID int64, ok bool) {
	s.mu.Lock()
	s.calls[r.URL.Path]++
	f, failing := s.failNext[r.URL.Path]
	delete(s.failNext, r.URL.Path)
	s.mu.Unlock()
	if failing && f.hang {
		// Read the body first: net/http only notices a client disconnect
		// once the request body has been consumed.
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
		return nil, 0, false
	}
	if failing {
		writeErr(w, f.status, f.code, f.message)
		return nil, 0, false
	}

	q := r.URL.Query()
	pid, _ := strconv.ParseInt(q.Get("partner_id"), 10, 64)
	ts, _ := strconv.ParseInt(q.Get("timestamp"), 10, 64)
	if pid != s.PartnerID {
		writeErr(w, 403, "invalid_partner_id", "Invalid partner_id, please have a check.")
		return nil, 0, false
	}
	if d := s.Now().Unix() - ts; d > 300 || d < -300 {
		writeErr(w, 403, "error_param", "Timestamp is expired.")
		return nil, 0, false
	}
	shopID, _ = strconv.ParseInt(q.Get("shop_id"), 10, 64)
	want := signer.Sign(s.PartnerKey, pid, r.URL.Path, ts, kind, q.Get("access_token"), shopID)
	if !signer.Equal(q.Get("sign"), want) {
		writeErr(w, 403, "error_sign", "Wrong sign.")
		return nil, 0, false
	}
	if kind == signer.Shop {
		s.mu.Lock()
		t, found := s.access[q.Get("access_token")]
		s.mu.Unlock()
		if !found || s.Now().After(t.expiresAt) || !contains(t.shopIDs, shopID) {
			writeErr(w, 403, "invalid_acceess_token", "Invalid access_token, please have a check.")
			return nil, 0, false
		}
	}
	if r.Method == http.MethodPost {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err := json.Unmarshal(b, &body); err != nil {
			writeErr(w, 400, "error_param", "Wrong parameters, detail: invalid json.")
			return nil, 0, false
		}
	}
	return body, shopID, true
}

func (s *Server) tokenGet(w http.ResponseWriter, r *http.Request) {
	body, _, ok := s.begin(w, r, signer.Public)
	if !ok {
		return
	}
	code, _ := body["code"].(string)
	s.mu.Lock()
	defer s.mu.Unlock()
	g, found := s.codes[code]
	delete(s.codes, code) // single use, even when the rest of the request is wrong
	if !found || s.Now().After(g.expiresAt) {
		writeErr(w, 403, "invalid_code", "The code is expired or used or invalid, please check the code.")
		return
	}
	shopID := num(body["shop_id"])
	mainID := num(body["main_account_id"])
	resp := map[string]any{"error": "", "message": "", "request_id": random()[:16], "expire_in": 14400}
	var shops []int64
	switch {
	case g.mainAccountID != 0 && mainID == g.mainAccountID:
		shops = g.shopIDs
		resp["shop_id_list"] = shops
		resp["merchant_id_list"] = []int64{}
	case g.shopID != 0 && shopID == g.shopID:
		shops = []int64{shopID}
	default:
		writeErr(w, 403, "invalid_shop_id", "The code or shop_id is wrong, please check the code or the shop_id.")
		return
	}
	// One pair for the whole authorization; each shop then refreshes on its own.
	at, rt := random(), random()
	s.access[at] = token{shopIDs: shops, expiresAt: s.Now().Add(4 * time.Hour)}
	s.refresh[rt] = append([]int64(nil), shops...)
	resp["access_token"], resp["refresh_token"] = at, rt
	writeJSON(w, resp)
}

func (s *Server) refreshGet(w http.ResponseWriter, r *http.Request) {
	body, _, ok := s.begin(w, r, signer.Public)
	if !ok {
		return
	}
	if s.RefreshDelay > 0 {
		time.Sleep(s.RefreshDelay)
	}
	rt, _ := body["refresh_token"].(string)
	shopID := num(body["shop_id"])
	s.mu.Lock()
	defer s.mu.Unlock()
	allowed := s.refresh[rt]
	if !contains(allowed, shopID) {
		writeErr(w, 403, "error_shop_refresh_token", "Your refresh token is error ,please check refresh token or shopid.")
		return
	}
	s.refresh[rt] = remove(allowed, shopID) // single use per shop
	at, nrt := random(), random()
	s.access[at] = token{shopIDs: []int64{shopID}, expiresAt: s.Now().Add(4 * time.Hour)}
	s.refresh[nrt] = []int64{shopID}
	writeJSON(w, map[string]any{"error": "", "message": "", "request_id": random()[:16],
		"partner_id": s.PartnerID, "shop_id": shopID, "access_token": at, "refresh_token": nrt, "expire_in": 14400})
}

func (s *Server) shopInfo(w http.ResponseWriter, r *http.Request) {
	_, shopID, ok := s.begin(w, r, signer.Shop)
	if !ok {
		return
	}
	s.mu.Lock()
	sh, found := s.shops[shopID]
	s.mu.Unlock()
	if !found {
		writeErr(w, 200, "error_shop", "shopid is invalid")
		return
	}
	writeJSON(w, map[string]any{"error": "", "message": "", "request_id": random()[:16],
		"shop_name": sh.Name, "region": sh.Region, "status": sh.Status, "is_cb": false, "is_sip": false,
		"is_upgraded_cbsc": false, "merchant_id": nil, "auth_time": s.Now().Add(-time.Hour).Unix(),
		"expire_time": sh.ExpireTime.Unix(), "shop_fulfillment_flag": "Others"})
}

func (s *Server) profile(w http.ResponseWriter, r *http.Request) {
	_, shopID, ok := s.begin(w, r, signer.Shop)
	if !ok {
		return
	}
	s.mu.Lock()
	sh := s.shops[shopID]
	s.mu.Unlock()
	if sh == nil {
		writeErr(w, 200, "error_shop", "shopid is invalid")
		return
	}
	writeJSON(w, map[string]any{"error": "", "message": "", "request_id": random()[:16],
		"response": map[string]any{"shop_logo": sh.Logo, "description": sh.Description, "shop_name": sh.Name}})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": code, "message": msg, "request_id": random()[:16]})
}

func random() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func num(v any) int64 {
	f, _ := v.(float64)
	return int64(f)
}

func contains(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func remove(ids []int64, id int64) []int64 {
	out := ids[:0:0]
	for _, x := range ids {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}
