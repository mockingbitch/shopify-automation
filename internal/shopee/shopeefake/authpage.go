package shopeefake

import (
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strconv"
)

// The seller authorization page of the fake (local development only). Real
// Shopee shows a login and consent screen; this one lists the fake shops
// and redirects back with a code, like the real flow.
var authTmpl = template.Must(template.New("auth").Parse(`<!doctype html>
<html lang="vi"><head><meta charset="utf-8"><title>FAKE Shopee – Ủy quyền</title>
<style>body{font-family:system-ui,sans-serif;max-width:40rem;margin:3rem auto;padding:0 1rem}
.banner{background:#fde68a;border:1px solid #d97706;padding:.75rem;border-radius:.5rem}
button{background:#c2410c;color:#fff;border:0;padding:.5rem 1rem;border-radius:.375rem;cursor:pointer;margin:.25rem 0}</style></head>
<body><p class="banner"><strong>FAKE Shopee</strong> dùng để phát triển trên máy. Không có shop hay tài khoản thật nào liên quan.</p>
<h1>Ủy quyền một shop thử nghiệm</h1>
<p>Partner {{.PartnerID}} yêu cầu quyền truy cập. Chuyển hướng về: <code>{{.Redirect}}</code></p>
{{range .Shops}}<form method="post" action="/auth">
<input type="hidden" name="redirect_uri" value="{{$.Redirect}}"><input type="hidden" name="state" value="{{$.State}}">
<input type="hidden" name="shop_id" value="{{.ID}}">
<button type="submit">Ủy quyền {{.Name}} ({{.ID}}, {{.Region}})</button></form>{{end}}
</body></html>`))

func (s *Server) authPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("partner_id") != strconv.FormatInt(s.PartnerID, 10) {
		http.Error(w, "partner_id không xác định", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	shops := make([]Shop, 0, len(s.shops))
	for _, sh := range s.shops {
		shops = append(shops, *sh)
	}
	s.mu.Unlock()
	sort.Slice(shops, func(i, j int) bool { return shops[i].ID < shops[j].ID })
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = authTmpl.Execute(w, map[string]any{"PartnerID": s.PartnerID, "Redirect": q.Get("redirect_uri"), "State": q.Get("state"), "Shops": shops})
}

func (s *Server) authGrant(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "biểu mẫu không hợp lệ", http.StatusBadRequest)
		return
	}
	shopID, err := strconv.ParseInt(r.PostFormValue("shop_id"), 10, 64)
	redirect, perr := url.Parse(r.PostFormValue("redirect_uri"))
	if err != nil || perr != nil || (redirect.Scheme != "http" && redirect.Scheme != "https") {
		http.Error(w, "yêu cầu không hợp lệ", http.StatusBadRequest)
		return
	}
	q := redirect.Query()
	q.Set("code", s.Authorize(shopID))
	q.Set("shop_id", strconv.FormatInt(shopID, 10))
	q.Set("state", r.PostFormValue("state"))
	redirect.RawQuery = q.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound) //nolint:gosec // G710: local development fake; redirects to the caller-supplied OAuth redirect_uri like Shopee does
}
