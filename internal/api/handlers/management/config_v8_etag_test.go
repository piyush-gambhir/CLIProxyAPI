package management

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)




func TestConfigV8ETagIfMatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := "config-version: 8\nserver: {port: 8317}\naccess: {api-keys: [revoked-key]}\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{cfg: cfg, configFilePath: path}
	router := gin.New()
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		router.Handle(method, "/v8/management/config", h.ConfigV8)
		router.Handle(method, "/v8/management/config.yaml", h.ConfigV8)
		router.Handle(method, "/v8/management/config/*path", h.ConfigV8)
	}

	do := func(method, url, body, ifMatch string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(method, url, strings.NewReader(body))
		if ifMatch != "" {
			req.Header.Set("If-Match", ifMatch)
		}
		router.ServeHTTP(recorder, req)
		return recorder
	}
	wantStatus := func(recorder *httptest.ResponseRecorder, status int) {
		t.Helper()
		if recorder.Code != status {
			t.Fatalf("status=%d want %d body=%s", recorder.Code, status, recorder.Body.String())
		}
	}
	fileBytes := func() []byte {
		t.Helper()
		data, errRead := os.ReadFile(path)
		if errRead != nil {
			t.Fatal(errRead)
		}
		return data
	}
	fileETag := func() string { return `"` + config.ComputeSourceHash(fileBytes()) + `"` }



	recorder := do(http.MethodPatch, "/v8/management/config", `{"server":{"port":8319}}`, `"stale"`)
	wantStatus(recorder, http.StatusPreconditionFailed)
	if string(fileBytes()) != raw {
		t.Fatal("stale PATCH changed the file")
	}

	recorder = do(http.MethodPatch, "/v8/management/config", `{"server":{"port":8319}}`, fileETag())
	wantStatus(recorder, http.StatusOK)
	if got := recorder.Header().Get("ETag"); got != fileETag() {
		t.Fatalf("PATCH response ETag=%q want %q", got, fileETag())
	}
	loaded, err := config.LoadConfig(path)
	if err != nil || loaded.Port != 8319 {
		t.Fatalf("PATCH with matching If-Match not applied: port=%d err=%v", loaded.Port, err)
	}


	get := do(http.MethodGet, "/v8/management/config.yaml", "", "")
	wantStatus(get, http.StatusOK)
	etag := get.Header().Get("ETag")
	if etag != fileETag() {
		t.Fatalf("GET config.yaml ETag=%q want %q", etag, fileETag())
	}
	subPathGet := do(http.MethodGet, "/v8/management/config/server", "", "")
	wantStatus(subPathGet, http.StatusOK)
	if got := subPathGet.Header().Get("ETag"); got != etag {
		t.Fatalf("GET sub-path ETag=%q want %q", got, etag)
	}


	revoked := do(http.MethodPut, "/v8/management/config/access/api-keys", `["other-key"]`, "")
	wantStatus(revoked, http.StatusOK)
	if got := revoked.Header().Get("ETag"); got != fileETag() {
		t.Fatalf("PUT response ETag=%q want %q", got, fileETag())
	}


	before := string(fileBytes())
	stale := do(http.MethodPut, "/v8/management/config.yaml", raw, etag)
	wantStatus(stale, http.StatusPreconditionFailed)
	if got := stale.Header().Get("ETag"); got != fileETag() {
		t.Fatalf("412 response ETag=%q want current %q", got, fileETag())
	}
	if string(fileBytes()) != before {
		t.Fatal("stale PUT changed the file")
	}
	loaded, err = config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.APIKeys) != 1 || loaded.APIKeys[0] != "other-key" {
		t.Fatalf("stale PUT resurrected revoked key: %v", loaded.APIKeys)
	}


	staleDelete := do(http.MethodDelete, "/v8/management/config/access/api-keys", "", etag)
	wantStatus(staleDelete, http.StatusPreconditionFailed)
	if string(fileBytes()) != before {
		t.Fatal("stale DELETE changed the file")
	}





	for _, ifMatch := range []string{"W/" + fileETag(), "not-an-etag", `"unterminated`, `"a"b"`, `"stale,*,tag"`, `W/"stale,*,tag"`, `"stale", *`} {
		recorder = do(http.MethodPut, "/v8/management/config.yaml", before, ifMatch)
		wantStatus(recorder, http.StatusPreconditionFailed)
	}


	recorder = httptest.NewRecorder()
	splitReq := httptest.NewRequest(http.MethodPut, "/v8/management/config/server/port", strings.NewReader(`8318`))
	splitReq.Header.Add("If-Match", `"stale"`)
	splitReq.Header.Add("If-Match", fileETag())
	router.ServeHTTP(recorder, splitReq)
	wantStatus(recorder, http.StatusOK)



	quotedComma := `"` + config.ComputeSourceHash(fileBytes()) + `,"`
	recorder = do(http.MethodPut, "/v8/management/config/server/port", `8317`, quotedComma)
	wantStatus(recorder, http.StatusPreconditionFailed)




	for _, blank := range []string{"", "  \t  "} {
		recorder = httptest.NewRecorder()
		blankReq := httptest.NewRequest(http.MethodPut, "/v8/management/config/server/port", strings.NewReader(`8317`))
		blankReq.Header.Add("If-Match", blank)
		router.ServeHTTP(recorder, blankReq)
		wantStatus(recorder, http.StatusPreconditionFailed)
	}


	current := fileETag()
	listed := do(http.MethodDelete, "/v8/management/config/access/api-keys", "", `"stale", `+current+` , W/`+current)
	wantStatus(listed, http.StatusOK)
	loaded, err = config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.APIKeys) != 0 {
		t.Fatalf("DELETE with matching If-Match list not applied: %v", loaded.APIKeys)
	}


	star := do(http.MethodPut, "/v8/management/config/server/port", `8320`, "*")
	wantStatus(star, http.StatusOK)


	first := do(http.MethodPut, "/v8/management/config/server/port", `8321`, fileETag())
	wantStatus(first, http.StatusOK)
	nextETag := first.Header().Get("ETag")
	if nextETag == "" || nextETag != fileETag() {
		t.Fatalf("PUT response ETag=%q want on-disk hash %q", nextETag, fileETag())
	}
	second := do(http.MethodPut, "/v8/management/config/server/port", `8322`, nextETag)
	wantStatus(second, http.StatusOK)


	unconditional := do(http.MethodPut, "/v8/management/config/server/port", `8323`, "")
	wantStatus(unconditional, http.StatusOK)


	missing := do(http.MethodGet, "/v8/management/config/no-such-key", "", "")
	wantStatus(missing, http.StatusNotFound)
	if got := missing.Header().Get("ETag"); got != "" {
		t.Fatalf("404 ETag=%q want empty", got)
	}
}
