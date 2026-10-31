// Command loadtest seeds a development database and measures the API under
// concurrent load (NFR-1, NFR-5) and the import throughput (NFR-6).
//
//	loadtest seed   -orgs 500 -shops 10           seed orgs, users, shops, products, runs
//	loadtest http   -users 50 -duration 60s       concurrent GETs as seeded users
//	loadtest import -rows 10000                   upload a CSV and time the import
//
// It is a development tool: seeding writes directly with DATABASE_MIGRATE_URL
// (app_owner) and must never be pointed at production. Seeded data is marked
// with the "lt-" slug prefix and is removed by `loadtest clean`.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/identity"
)

const password = "load test password 2026"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: loadtest seed|http|import|clean [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "seed":
		err = seed(os.Args[2:])
	case "http":
		err = load(os.Args[2:])
	case "import":
		err = importRun(os.Args[2:])
	case "clean":
		err = clean()
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "loadtest:", err)
		os.Exit(1)
	}
}

func ownerConn(ctx context.Context) (*pgx.Conn, error) {
	url := os.Getenv("DATABASE_MIGRATE_URL")
	if url == "" {
		return nil, errors.New("DATABASE_MIGRATE_URL is required")
	}
	if os.Getenv("APP_ENV") == "production" {
		return nil, errors.New("refusing to seed a production environment")
	}
	return pgx.Connect(ctx, url)
}

func seed(args []string) error {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	orgs := fs.Int("orgs", 500, "organizations")
	shops := fs.Int("shops", 10, "shops per organization")
	products := fs.Int("products", 50, "products per organization")
	runs := fs.Int("runs", 20, "workflow runs per organization")
	_ = fs.Parse(args)
	ctx := context.Background()
	conn, err := ownerConn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()
	hash, err := identity.Hasher{Params: identity.DefaultArgon2}.Hash(password)
	if err != nil {
		return err
	}
	start := time.Now()
	app := uuid.New()
	if _, err := conn.Exec(ctx, `INSERT INTO shopee_apps (id, name, environment, api_host, auth_host, partner_id, partner_key_enc, dek_wrapped, kek_id,
		app_type, redirect_uri, enabled) VALUES ($1, 'lt-app', 'sandbox', 'https://lt.invalid', 'https://lt.invalid', 1, '\x00', '\x00', 'none', 'ERP System',
		'https://lt.invalid/cb', false)`, app); err != nil {
		return err
	}
	shopID := int64(9_000_000_000)
	for o := 0; o < *orgs; o++ {
		user, org := uuid.New(), uuid.New()
		b := &pgx.Batch{}
		b.Queue(`INSERT INTO users (id, email, password_hash, display_name, email_verified_at) VALUES ($1, $2, $3, 'Load', now())`,
			user, fmt.Sprintf("lt-%d@load.invalid", o), hash)
		b.Queue(`INSERT INTO organizations (id, name, slug, created_by) VALUES ($1, $2, $3, $4)`, org, fmt.Sprintf("LT %d", o), fmt.Sprintf("lt-%d", o), user)
		b.Queue(`INSERT INTO organization_members (id, org_id, user_id, role_key) VALUES ($1, $2, $3, 'owner')`, uuid.New(), org, user)
		var shopIDs []uuid.UUID
		for s := 0; s < *shops; s++ {
			id := uuid.New()
			shopIDs = append(shopIDs, id)
			shopID++
			status := "ACTIVE"
			if s%10 == 9 {
				status = "RECONNECT_REQUIRED"
			}
			b.Queue(`INSERT INTO shop_connections (id, org_id, shopee_app_id, shopee_shop_id, status, region, shop_name) VALUES ($1, $2, $3, $4, $5, 'VN', $6)`,
				id, org, app, shopID, status, fmt.Sprintf("Shop %d", shopID))
		}
		for p := 0; p < *products; p++ {
			b.Queue(`INSERT INTO products (id, org_id, sku, name, description, price, currency, stock, weight_kg, created_by)
				VALUES ($1, $2, $3, $4, 'Load test product', 100000, 'VND', 10, 0.5, $5)`, uuid.New(), org, fmt.Sprintf("LT-%d-%d", o, p), fmt.Sprintf("Product %d", p), user)
		}
		for r := 0; r < *runs; r++ {
			status := []string{"COMPLETED", "COMPLETED", "FAILED", "WAITING_APPROVAL"}[r%4]
			b.Queue(`INSERT INTO workflow_runs (id, org_id, shop_connection_id, definition_key, definition_version, status, input, created_by)
				VALUES ($1, $2, $3, 'SHOP_SETUP', 1, $4, '{}', $5)`, uuid.New(), org, shopIDs[r%len(shopIDs)], status, user)
		}
		if err := conn.SendBatch(ctx, b).Close(); err != nil {
			return fmt.Errorf("org %d: %w", o, err)
		}
	}
	fmt.Printf("seeded %d orgs, %d shops, %d products, %d runs in %s\n", *orgs, *orgs**shops, *orgs**products, *orgs**runs, time.Since(start).Round(time.Millisecond))
	return nil
}

func clean() error {
	ctx := context.Background()
	conn, err := ownerConn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()
	for _, q := range []string{
		`DELETE FROM workflow_events WHERE org_id IN (SELECT id FROM organizations WHERE slug LIKE 'lt-%')`,
		`DELETE FROM organizations WHERE slug LIKE 'lt-%'`,
		`DELETE FROM shopee_apps WHERE name = 'lt-app'`,
		`DELETE FROM users WHERE email LIKE 'lt-%@load.invalid'`,
	} {
		if _, err := conn.Exec(ctx, q); err != nil {
			return err
		}
	}
	fmt.Println("load test data removed")
	return nil
}

// origin is the dashboard origin the API accepts (same-origin check).
func origin() string {
	u := os.Getenv("PUBLIC_BASE_URL")
	if u == "" {
		u = "http://localhost:3100"
	}
	return strings.TrimRight(u, "/")
}

// session is a signed-in API client.
type session struct {
	http *http.Client
	csrf string
	org  string
	base string
}

func login(base string, i int) (*session, error) {
	jar, _ := cookiejar.New(nil)
	s := &session{http: &http.Client{Jar: jar, Timeout: 30 * time.Second}, base: base}
	body, _ := json.Marshal(map[string]string{"email": fmt.Sprintf("lt-%d@load.invalid", i), "password": password})
	req, _ := http.NewRequestWithContext(context.Background(), "POST", base+"/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin())
	res, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	var out struct {
		Data struct {
			CSRF string `json:"csrf_token"`
		} `json:"data"`
	}
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("login %d: %d %s", i, res.StatusCode, b)
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	s.csrf = out.Data.CSRF
	r, err := s.get("/api/v1/auth/session")
	if err != nil {
		return nil, err
	}
	var sess struct {
		Data struct {
			Memberships []struct {
				Organization struct {
					ID string `json:"id"`
				} `json:"organization"`
			} `json:"memberships"`
		} `json:"data"`
	}
	_ = json.Unmarshal(r, &sess)
	if len(sess.Data.Memberships) == 0 {
		return nil, fmt.Errorf("user %d has no organization", i)
	}
	s.org = sess.Data.Memberships[0].Organization.ID
	return s, nil
}

func (s *session) get(path string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(context.Background(), "GET", s.base+path, nil)
	res, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return b, fmt.Errorf("%s: %d", path, res.StatusCode)
	}
	return b, nil
}

func load(args []string) error {
	fs := flag.NewFlagSet("http", flag.ExitOnError)
	base := fs.String("base", "http://localhost:8080", "API origin")
	users := fs.Int("users", 50, "concurrent users (seeded orgs 0..n-1)")
	duration := fs.Duration("duration", 60*time.Second, "test length")
	_ = fs.Parse(args)
	var sessions []*session
	for i := 0; i < *users; i++ {
		s, err := login(*base, i)
		if err != nil && strings.Contains(err.Error(), ": 429 ") {
			// Logins are rate limited per IP (30/min): wait for the window.
			fmt.Println("login rate limit reached; waiting 61s")
			time.Sleep(61 * time.Second)
			s, err = login(*base, i)
		}
		if err != nil {
			return err
		}
		sessions = append(sessions, s)
	}
	paths := []string{"/shops", "/overview", "/products", "/workflows", "/notifications", "/approvals?status=PENDING", "/imports", "/audit-logs?limit=20"}
	type sample struct {
		path string
		d    time.Duration
		err  bool
	}
	var mu sync.Mutex
	var samples []sample
	deadline := time.Now().Add(*duration)
	var wg sync.WaitGroup
	for _, s := range sessions {
		wg.Add(1)
		go func(s *session) {
			defer wg.Done()
			for time.Now().Before(deadline) {
				p := paths[rand.IntN(len(paths))] // #nosec G404 -- load mix, not security
				t0 := time.Now()
				_, err := s.get("/api/v1/orgs/" + s.org + p)
				d := time.Since(t0)
				mu.Lock()
				samples = append(samples, sample{p, d, err != nil})
				mu.Unlock()
			}
		}(s)
	}
	wg.Wait()
	report := func(name string, ds []time.Duration, errs int) {
		slices.Sort(ds)
		q := func(f float64) time.Duration { return ds[int(f*float64(len(ds)-1))] }
		fmt.Printf("%-28s n=%-6d err=%-4d p50=%-8s p95=%-8s p99=%-8s max=%s\n", name, len(ds), errs,
			q(0.5).Round(time.Millisecond/10), q(0.95).Round(time.Millisecond/10), q(0.99).Round(time.Millisecond/10), ds[len(ds)-1].Round(time.Millisecond))
	}
	byPath := map[string][]time.Duration{}
	errsBy := map[string]int{}
	var all []time.Duration
	errs := 0
	for _, s := range samples {
		byPath[s.path] = append(byPath[s.path], s.d)
		all = append(all, s.d)
		if s.err {
			errsBy[s.path]++
			errs++
		}
	}
	fmt.Printf("%d users, %s, %.0f requests/s\n", *users, *duration, float64(len(samples))/duration.Seconds())
	for _, p := range paths {
		if len(byPath[p]) > 0 {
			report(strings.SplitN(p, "?", 2)[0], byPath[p], errsBy[p])
		}
	}
	report("ALL", all, errs)
	return nil
}

func importRun(args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	base := fs.String("base", "http://localhost:8080", "API origin")
	rows := fs.Int("rows", 10000, "rows")
	_ = fs.Parse(args)
	s, err := login(*base, 0)
	if err != nil {
		return err
	}
	var csv strings.Builder
	csv.WriteString("sku,name,description,category,price,currency,stock,weight_kg\n")
	for i := 0; i < *rows; i++ {
		// Valid except for images: rows are parsed and validated, then
		// stop before any network download.
		fmt.Fprintf(&csv, "IMP-%d,Imported product %d,Load test,100017,%d,VND,%d,0.4\n", i, i, 10000+i, i%50)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "load.csv")
	_, _ = fw.Write([]byte(csv.String()))
	_ = mw.Close()
	req, _ := http.NewRequestWithContext(context.Background(), "POST", s.base+"/api/v1/orgs/"+s.org+"/imports", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-CSRF-Token", s.csrf)
	req.Header.Set("Origin", origin())
	start := time.Now()
	res, err := s.http.Do(req)
	if err != nil {
		return err
	}
	b, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 202 {
		return fmt.Errorf("upload: %d %s", res.StatusCode, b)
	}
	var batch struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(b, &batch)
	for {
		time.Sleep(500 * time.Millisecond)
		r, err := s.get("/api/v1/orgs/" + s.org + "/imports/" + batch.Data.ID)
		if err != nil {
			return err
		}
		var st struct {
			Data struct {
				Status    string `json:"status"`
				Total     int    `json:"total_rows"`
				Invalid   int    `json:"invalid_rows"`
				Imported  int    `json:"imported_rows"`
				ErrorText string `json:"error"`
			} `json:"data"`
		}
		_ = json.Unmarshal(r, &st)
		if st.Data.Status == "COMPLETED" || st.Data.Status == "FAILED" {
			fmt.Printf("import of %d rows: %s in %s (total=%d invalid=%d imported=%d %s)\n", *rows, st.Data.Status,
				time.Since(start).Round(time.Millisecond), st.Data.Total, st.Data.Invalid, st.Data.Imported, st.Data.ErrorText)
			return nil
		}
		if time.Since(start) > 10*time.Minute {
			return errors.New("import did not finish in 10 minutes")
		}
	}
}
