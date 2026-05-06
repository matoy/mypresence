package testhelper

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"presence-app/internal/db"
	"presence-app/internal/models"
)

type fakeFataler struct {
	failed bool
	msg    string
}

type fatalStop struct{}

type errTransport struct{}

func (errTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("transport error")
}

func (f *fakeFataler) Helper() {}

func (f *fakeFataler) Fatalf(format string, args ...interface{}) {
	f.failed = true
	f.msg = format
	panic(fatalStop{})
}

func expectFatal(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected fatal stop")
		}
		if _, ok := r.(fatalStop); !ok {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	fn()
}

func newEnvWithEchoServer(t *testing.T) *Env {
	t.Helper()
	env := NewEnv(t)
	h := http.NewServeMux()

	h.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	h.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	h.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.FormValue("username") == "admin" && r.FormValue("password") == "admin" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	})

	env.StartServer(t, h)
	return env
}

func TestNewEnvAndURL(t *testing.T) {
	env := NewEnv(t)
	if env.DB == nil || env.Cfg == nil || env.Client == nil {
		t.Fatalf("expected initialized env")
	}
	if env.Cfg.AdminUser != "admin" {
		t.Fatalf("unexpected admin user: %q", env.Cfg.AdminUser)
	}

	env.StartServer(t, http.NewServeMux())
	if !strings.HasSuffix(env.URL("/x"), "/x") {
		t.Fatalf("unexpected URL: %s", env.URL("/x"))
	}

	if err := env.Client.CheckRedirect(httptest.NewRequest(http.MethodGet, "/", nil), make([]*http.Request, 1)); err != nil {
		t.Fatalf("unexpected redirect err: %v", err)
	}
	if err := env.Client.CheckRedirect(httptest.NewRequest(http.MethodGet, "/", nil), make([]*http.Request, 10)); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("expected ErrUseLastResponse, got %v", err)
	}
}

func TestNewEnvFailurePaths(t *testing.T) {
	t.Run("open db fails", func(t *testing.T) {
		origOpenDB := openDB
		openDB = func(dir string) (*db.DB, error) {
			return nil, errors.New("open failure")
		}
		t.Cleanup(func() { openDB = origOpenDB })

		tb := &fakeFataler{}
		expectFatal(t, func() {
			_ = newEnv(tb, t.TempDir())
		})
		if !tb.failed {
			t.Fatalf("expected fatal path")
		}
	})

	t.Run("seed defaults fails", func(t *testing.T) {
		origSeedDefaults := seedDefaults
		seedDefaults = func(database *db.DB, adminUser, adminPassword string) error {
			return errors.New("seed failure")
		}
		t.Cleanup(func() { seedDefaults = origSeedDefaults })

		tb := &fakeFataler{}
		expectFatal(t, func() {
			_ = newEnv(tb, t.TempDir())
		})
		if !tb.failed {
			t.Fatalf("expected fatal path")
		}
	})
}

func TestDoAndDoForm(t *testing.T) {
	env := newEnvWithEchoServer(t)

	resp := env.Do(http.MethodGet, "/ok", nil)
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Do status = %d", resp.StatusCode)
	}

	formResp := env.DoForm(http.MethodPost, "/login", strings.NewReader("username=admin&password=admin"))
	defer formResp.Body.Close() //nolint:errcheck
	if formResp.StatusCode != http.StatusOK {
		t.Fatalf("DoForm status = %d", formResp.StatusCode)
	}
}

func TestDoAndDoFormPanicsOnBadMethod(t *testing.T) {
	env := NewEnv(t)
	env.StartServer(t, http.NewServeMux())

	t.Run("Do", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatalf("expected panic for invalid method")
			}
		}()
		_ = env.Do("\n", "/", nil)
	})

	t.Run("DoForm", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatalf("expected panic for invalid method")
			}
		}()
		_ = env.DoForm("\n", "/", nil)
	})
}

func TestDoAndDoFormPanicsOnTransportError(t *testing.T) {
	env := NewEnv(t)
	env.StartServer(t, http.NewServeMux())
	env.Client.Transport = errTransport{}

	t.Run("Do", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatalf("expected panic on transport error")
			}
		}()
		_ = env.Do(http.MethodGet, "/", nil)
	})

	t.Run("DoForm", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatalf("expected panic on transport error")
			}
		}()
		_ = env.DoForm(http.MethodPost, "/", strings.NewReader("x=1"))
	})
}

func TestDoJSONAndMustDecodeJSON(t *testing.T) {
	env := newEnvWithEchoServer(t)
	resp := env.DoJSON(http.MethodPost, "/json", map[string]any{"k": "v"})

	var got map[string]string
	MustDecodeJSON(t, resp, &got)
	if got["status"] != "ok" {
		t.Fatalf("unexpected json payload: %#v", got)
	}
}

func TestMustDecodeJSONErrorPath(t *testing.T) {
	tb := &fakeFataler{}
	resp := &http.Response{Body: io.NopCloser(bytes.NewBufferString("{"))}
	expectFatal(t, func() {
		mustDecodeJSON(tb, resp, &map[string]any{})
	})
	if !tb.failed {
		t.Fatalf("expected fatal path")
	}
}

func TestLoginSessionSuccess(t *testing.T) {
	env := newEnvWithEchoServer(t)
	env.LoginSession(t, "admin", "admin")
}

func TestLoginSessionFailurePath(t *testing.T) {
	env := newEnvWithEchoServer(t)
	tb := &fakeFataler{}
	expectFatal(t, func() {
		loginSession(tb, env, "admin", "wrong")
	})
	if !tb.failed {
		t.Fatalf("expected fatal path")
	}
}

func TestInjectUserAndCreateHelpers(t *testing.T) {
	env := NewEnv(t)
	env.StartServer(t, http.NewServeMux())

	u := env.GetAdminUser(t)
	if u == nil || u.Email == "" {
		t.Fatalf("expected seeded admin user")
	}

	created := env.CreateBasicUser(t, "basic@example.com", "Basic", "secret")
	if created == nil || created.Email != "basic@example.com" {
		t.Fatalf("unexpected created user: %#v", created)
	}

	tok := env.InjectUser(t, created)
	if tok == "" {
		t.Fatalf("expected session token")
	}
}

func TestInjectUserWithoutServer(t *testing.T) {
	env := NewEnv(t)
	u := env.GetAdminUser(t)
	tok := env.InjectUser(t, u)
	if tok == "" {
		t.Fatalf("expected session token")
	}
}

func TestInjectUserCreateSessionError(t *testing.T) {
	env := NewEnv(t)
	u := env.GetAdminUser(t)
	tb := &fakeFataler{}

	orig := createSession
	createSession = func(database *db.DB, userID int64) (string, error) {
		return "", errors.New("boom")
	}
	t.Cleanup(func() { createSession = orig })

	expectFatal(t, func() {
		_ = injectUser(tb, env, u)
	})
	if !tb.failed {
		t.Fatalf("expected fatal path")
	}
}

func TestCreateBasicUserAndGetAdminErrorPaths(t *testing.T) {
	env := NewEnv(t)

	origCreateLocalUser := createLocalUser
	origGetUserByID := getUserByID
	origGetUserByEmail := getUserByEmail
	t.Cleanup(func() {
		createLocalUser = origCreateLocalUser
		getUserByID = origGetUserByID
		getUserByEmail = origGetUserByEmail
	})

	t.Run("CreateLocalUserError", func(t *testing.T) {
		tb := &fakeFataler{}
		createLocalUser = func(database *db.DB, email, name, password string) (int64, error) {
			return 0, errors.New("boom")
		}
		expectFatal(t, func() {
			_ = createBasicUser(tb, env, "a@b.c", "n", "p")
		})
		if !tb.failed {
			t.Fatalf("expected fatal path")
		}
	})

	t.Run("GetUserByIDError", func(t *testing.T) {
		tb := &fakeFataler{}
		createLocalUser = origCreateLocalUser
		getUserByID = func(database *db.DB, id int64) (*models.User, error) {
			return nil, errors.New("boom")
		}
		expectFatal(t, func() {
			_ = createBasicUser(tb, env, "a2@b.c", "n", "p")
		})
		if !tb.failed {
			t.Fatalf("expected fatal path")
		}
	})

	t.Run("GetUserByEmailError", func(t *testing.T) {
		tb := &fakeFataler{}
		getUserByEmail = func(database *db.DB, email string) (*models.User, error) {
			return nil, errors.New("boom")
		}
		expectFatal(t, func() {
			_ = getAdminUser(tb, env)
		})
		if !tb.failed {
			t.Fatalf("expected fatal path")
		}
	})
}

func TestWithUserInContextAndGetTestUser(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	u := &models.User{ID: 42, Name: "Test User"}
	req = WithUserInContext(req, u)
	got := GetTestUser(req)
	if got == nil || got.ID != 42 {
		t.Fatalf("unexpected context user: %#v", got)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	if GetTestUser(req2) != nil {
		t.Fatalf("expected nil user when not injected")
	}
}
