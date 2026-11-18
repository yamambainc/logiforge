package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yamambainc/logiforge/internal/backend"
	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/snapshot"
	"github.com/yamambainc/logiforge/internal/store"
)

// emptySnap は、repository のない置き場である。broken なら、一覧を読めない。
type emptySnap struct{ broken bool }

func (s emptySnap) Repositories(context.Context) ([]string, error) {
	if s.broken {
		return nil, errors.New("置き場が壊れている")
	}
	return nil, nil
}
func (emptySnap) Latest(context.Context, string) (string, error) { return "", ports.ErrNotFound }
func (emptySnap) Commit(context.Context, string, string) (snapshot.Commit, error) {
	return snapshot.Commit{}, ports.ErrNotFound
}
func (emptySnap) MainAt(context.Context, string, int) (string, error) { return "", ports.ErrNotFound }
func (emptySnap) Main(context.Context, string, string) (*snapshot.Main, error) {
	return nil, ports.ErrNotFound
}
func (emptySnap) Branch(context.Context, string, string) (*snapshot.Branch, error) {
	return nil, ports.ErrNotFound
}
func (emptySnap) BranchHistory(context.Context, string, string) (store.BranchHistory, error) {
	return store.BranchHistory{}, nil
}
func (emptySnap) Broken(context.Context, string) (*store.Broken, error) { return nil, nil }
func (emptySnap) Scanned(context.Context, string) (string, error)       { return "", nil }
func (emptySnap) MigratedTo(context.Context, string) (string, error)    { return "", nil }

func (emptySnap) Artifact(context.Context, string, string) ([]byte, error) {
	return nil, ports.ErrNotFound
}

type auth struct{}

func (auth) Authenticate(_ context.Context, token string) (ports.Principal, error) {
	if token != "good" {
		return ports.Principal{}, ports.ErrUnauthenticated
	}
	return ports.Principal{Login: "a", Kind: ports.PrincipalClient}, nil
}
func (auth) CanRead(context.Context, string, string) (bool, error)   { return true, nil }
func (auth) CanWrite(context.Context, string, string) (bool, error)  { return true, nil }
func (auth) CanCreate(context.Context, string, string) (bool, error) { return true, nil }

func TestProblems(t *testing.T) {
	srv := httptest.NewServer(NewHandler(&backend.Backend{Auth: auth{}, Snap: emptySnap{}}, nil))
	defer srv.Close()
	cases := []struct {
		token, path string
		status      int
		code        string
	}{
		{"", "/v1/repositories/q", 401, "unauthenticated"},
		{"bad", "/v1/repositories/q", 401, "unauthenticated"},
		{"good", "/v1/repositories/q", 404, "not_found"},
		{"good", "/v1/repositories/Q_Q", 404, "not_found"},
		{"good", "/v1/repositories?limit=500", 422, "validation_failed"},
		{"good", "/v1/repositories?limit=x", 422, "validation_failed"},
		{"good", "/v1/nothing", 404, "not_found"},
	}
	for _, c := range cases {
		req, _ := http.NewRequest("GET", srv.URL+c.path, nil)
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var p problem
		body, _ := io.ReadAll(res.Body)
		_ = json.Unmarshal(body, &p)
		if res.StatusCode != c.status || p.Code != c.code || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/problem+json") {
			t.Fatalf("%s %s: %d %s %s", c.token, c.path, res.StatusCode, res.Header.Get("Content-Type"), body)
		}
	}
	req, _ := http.NewRequest("GET", srv.URL+"/v1/repositories", nil)
	req.Header.Set("Authorization", "Bearer good")
	res, _ := http.DefaultClient.Do(req)
	var page struct {
		Items      []any   `json:"items"`
		NextCursor *string `json:"next_cursor"`
	}
	if err := json.NewDecoder(res.Body).Decode(&page); err != nil || res.StatusCode != 200 || page.Items == nil || page.NextCursor != nil {
		t.Fatalf("list: %d %+v %v", res.StatusCode, page, err)
	}
}

func TestInternalErrorIsProblem(t *testing.T) {
	srv := httptest.NewServer(NewHandler(&backend.Backend{Auth: auth{}, Snap: emptySnap{broken: true}}, nil))
	defer srv.Close()
	req, _ := http.NewRequest("GET", srv.URL+"/v1/repositories", nil)
	req.Header.Set("Authorization", "Bearer good")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var p problem
	_ = json.NewDecoder(res.Body).Decode(&p)
	if res.StatusCode != 500 || p.Code != "internal" || strings.Contains(p.Detail, "壊れている") {
		t.Fatalf("%d %+v", res.StatusCode, p)
	}
}

func TestHealthz(t *testing.T) {
	// 置き場も GitForge もつながない。ヘルスチェックは、それらに触れない。
	srv := httptest.NewServer(NewHandler(&backend.Backend{}, nil))
	defer srv.Close()
	res, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("healthz: %d", res.StatusCode)
	}
}
