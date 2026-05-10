package main

import (
	"crypto/subtle"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/matoy/myPresence/internal/config"
	"github.com/matoy/myPresence/internal/db"
	"github.com/matoy/myPresence/internal/i18n"
	"github.com/matoy/myPresence/internal/middleware"
	"github.com/matoy/myPresence/internal/models"
)

// buildTemplateFuncMap constructs the FuncMap used by all HTML templates.
func buildTemplateFuncMap(cfg *config.Config) template.FuncMap {
	return template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		// safehtml marks a string as safe HTML so html/template does not escape it.
		// Only use with strings originating from our own controlled i18n data.
		"safehtml": func(s string) template.HTML { return template.HTML(s) }, //nolint:gosec
		"seq": func(n int) []int {
			s := make([]int, n)
			for i := range s {
				s[i] = i
			}
			return s
		},
		"json": func(v interface{}) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
		"statusColor": func(statuses []models.Status, id int64) string {
			for _, s := range statuses {
				if s.ID == id {
					return s.Color
				}
			}
			return "#e5e7eb"
		},
		"statusName": func(statuses []models.Status, id int64) string {
			for _, s := range statuses {
				if s.ID == id {
					return s.Name
				}
			}
			return ""
		},
		"hasKey": func(m map[string]int64, key string) bool {
			_, ok := m[key]
			return ok
		},
		"getKey": func(m map[string]int64, key string) int64 {
			if m == nil {
				return 0
			}
			return m[key]
		},
		"getCount":    func(m map[int64]int, key int64) int { return m[key] },
		"getStrCount": func(m map[string]int, key string) int { return m[key] },
		"sumMap": func(m map[int64]int) int {
			total := 0
			for _, v := range m {
				total += v
			}
			return total
		},
		// Float64 variants for half-day support
		"getCountF":    tmplGetCountF,
		"getStrCountF": tmplGetStrCountF,
		"sumMapF":      tmplSumMapF,
		"fmtF":         tmplFmtF,
		"percentF":     tmplPercentF,
		"i2f":          tmplI2F,
		"subF":         tmplSubF,
		"activityRocket": func(notSet, onSiteDays, billableDays, projectActivity float64) bool {
			return tmplActivitySummaryRocket(notSet, onSiteDays, billableDays, projectActivity, cfg.OnsiteRatioThreshold)
		},
		// Presence half-day helpers for templates
		"presenceHalf":    tmplPresenceHalf,
		"hasDatePresence": tmplHasDatePresence,
		"dict": func(pairs ...interface{}) map[string]interface{} {
			d := make(map[string]interface{})
			for i := 0; i < len(pairs)-1; i += 2 {
				d[pairs[i].(string)] = pairs[i+1]
			}
			return d
		},
		"intToInt64": func(i int) int64 { return int64(i) },
		"upper":      strings.ToUpper,
		"percent":    tmplPercent,
		"hasRole": func(user *models.User, role string) bool {
			if user == nil {
				return false
			}
			return user.HasRole(role)
		},
	}
}

// loadTemplates parses all page templates with the given FuncMap.
// Calls log.Fatalf if any template fails to parse.
func loadTemplates(funcMap template.FuncMap) map[string]*template.Template {
	pages := []string{
		"login", "calendar", "admin_teams", "admin_statuses", "admin_activity",
		"admin_holidays", "admin_users", "admin_user_logs", "floorplan", "admin_floorplans",
		"pat", "settings_change_password", "forgot_password", "reset_password",
		"impersonate", "projects", "admin_projects", "admin_projects_report",
	}
	templates := make(map[string]*template.Template)
	for _, page := range pages {
		t, err := template.New("").Funcs(funcMap).ParseFS(
			templateFS,
			"web/templates/layout.html",
			"web/templates/"+page+".html",
		)
		if err != nil {
			log.Fatalf("Template parse error (%s): %v", page, err)
		}
		templates[page] = t
	}
	return templates
}

// newRenderPage returns a render function that resolves the current user,
// language, CSRF token and impersonation state before executing the named template.
func newRenderPage(cfg *config.Config, database *db.DB, templates map[string]*template.Template) func(http.ResponseWriter, *http.Request, string, interface{}) {
	return func(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
		user := middleware.GetUser(r)
		lang := i18n.LangFromRequest(r, cfg.DefaultLang)

		// Check if a logo file exists in the data directory.
		logoExists := false
		logoFile := "logo.png"
		if cfg.LogoPath != "" {
			logoFile = cfg.LogoPath
		}
		if _, err := os.Stat(filepath.Join(cfg.DataDir, logoFile)); err == nil {
			logoExists = true
		}

		var csrfToken string
		if cookie, err := r.Cookie("session"); err == nil {
			csrfToken = middleware.GenerateCSRFToken(cfg.SecretKey, cookie.Value)
		}

		// Detect impersonation: check if a real_session cookie is present and valid.
		var realAdmin *models.User
		if realCookie, err := r.Cookie("real_session"); err == nil {
			if adminUser, err := database.GetSessionUser(realCookie.Value); err == nil && adminUser.HasRole(models.RoleGlobal) {
				realAdmin = adminUser
			}
		}

		pd := models.PageData{
			Config: map[string]string{
				"AppName":        cfg.AppName,
				"PrimaryColor":   cfg.PrimaryColor,
				"SecondaryColor": cfg.SecondaryColor,
				"AccentColor":    cfg.AccentColor,
				"FontURL":        cfg.FontURL,
				"FontFamily":     cfg.FontFamily,
				"FontFamilyMono": cfg.FontFamilyMono,
			},
			User:              user,
			Page:              page,
			Data:              data,
			SAMLEnabled:       cfg.SAMLEnabled,
			SMTPEnabled:       cfg.SMTPURL != "",
			HideFooter:        cfg.HideFooter,
			AppVersion:        config.Version,
			DisableFloorplans: cfg.DisableFloorplans,
			DisableAPI:        cfg.DisableAPI,
			DisableProjects:   cfg.DisableProjects,
			T:                 i18n.T(lang),
			Lang:              lang,
			SupportedLangs:    i18n.Supported,
			CSRFToken:         csrfToken,
			RealAdmin:         realAdmin,
		}
		if logoExists {
			pd.Config.(map[string]string)["LogoURL"] = "/data/logo.png"
		}

		tmpl, ok := templates[page]
		if !ok {
			http.Error(w, "Template not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.ExecuteTemplate(w, "layout", pd); err != nil {
			log.Printf("Template render error: %v", err)
		}
	}
}

// floorplanImgHandler returns a handler that serves floorplan image files from
// dataDir. Only files whose name starts with "floorplan_" and have an allowed
// image extension are served.
func floorplanImgHandler(dataDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		if !strings.HasPrefix(name, "floorplan_") {
			http.NotFound(w, r)
			return
		}
		ext := strings.ToLower(filepath.Ext(name))
		allowed := map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}
		if !allowed[ext] {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dataDir, name))
	}
}

// dataFileHandler returns a handler that serves an allowlisted set of logo
// files from dataDir (logo.png, logo.svg, logo.jpg).
func dataFileHandler(dataDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		allowed := map[string]bool{"logo.png": true, "logo.svg": true, "logo.jpg": true}
		if !allowed[name] {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dataDir, name))
	}
}

// metricsHandler returns a handler that exposes Prometheus metrics behind a
// Bearer-token check. Returns 404 when metricsToken is empty (metrics disabled).
func metricsHandler(metricsToken string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if metricsToken == "" {
			http.Error(w, "Metrics not enabled", http.StatusNotFound)
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), []byte(metricsToken)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mypresence-metrics"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		promhttp.Handler().ServeHTTP(w, r)
	}
}

// langSwitcherHandler returns a handler that sets the "lang" cookie to a
// supported language code and redirects the user back to the same page
// (same-origin only — open redirect is prevented).
func langSwitcherHandler(defaultLang string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lang := r.FormValue("lang")
		valid := false
		for _, s := range i18n.Supported {
			if s.Code == lang {
				valid = true
				break
			}
		}
		if !valid {
			lang = defaultLang
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "lang",
			Value:    lang,
			Path:     "/",
			MaxAge:   365 * 24 * 3600,
			SameSite: http.SameSiteLaxMode,
			HttpOnly: true,
		})
		// Prevent open redirect: only allow same-origin redirects.
		target := "/"
		if ref := r.Header.Get("Referer"); ref != "" {
			if u, err := url.Parse(ref); err == nil {
				if p := u.RequestURI(); strings.HasPrefix(p, "/") {
					target = p
				}
			}
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
}
