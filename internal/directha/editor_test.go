package directha

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEditorRoundTripAndRequestGuards(t *testing.T) {
	c, err := New(sample("http://192.168.1.11:8123"), "private-ha-token")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	doc := RulesDocument{Revision: "revision-1", Rules: c.Rules()}
	handler, err := NewEditor(c, EditorStore{Read: func() (RulesDocument, error) { return doc, nil }, Save: func(rev string, rules []Rule) (RulesDocument, error) {
		calls++
		if rev != doc.Revision {
			return RulesDocument{}, ErrConflict
		}
		if err := c.ReplaceRules(rules); err != nil {
			return RulesDocument{}, ErrInvalidRules
		}
		doc = RulesDocument{Revision: "revision-2", Rules: c.Rules()}
		return doc, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	response, err := http.Get(server.URL + "/api/rules")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if bytes.Contains(data, []byte("private-ha-token")) {
		t.Fatal("HA token leaked")
	}
	var loaded struct {
		RulesDocument
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(data, &loaded); err != nil || loaded.CSRF == "" {
		t.Fatal("missing page token", err)
	}
	candidate := sample("http://192.168.1.11").Rules
	candidate[0].Phrases = []string{"включи ночник"}
	candidate[0].EntityID = "switch.nochnik"
	payload, _ := json.Marshal(RulesDocument{Revision: loaded.Revision, Rules: candidate})
	for _, tc := range []struct {
		name, origin, token, host, contentType, body string
		code                                         int
	}{
		{"cross origin", "https://evil.example", loaded.CSRF, "", "application/json", string(payload), 403},
		{"no origin", "", loaded.CSRF, "", "application/json", string(payload), 403},
		{"no csrf", server.URL, "", "", "application/json", string(payload), 403},
		{"rebinding", server.URL, loaded.CSRF, "evil.example", "application/json", string(payload), 403},
		{"form", server.URL, loaded.CSRF, "", "text/plain", string(payload), 415},
		{"unknown field", server.URL, loaded.CSRF, "", "application/json", `{"revision":"revision-1","url":"http://8.8.8.8","rules":[]}`, 400},
		{"trailing JSON", server.URL, loaded.CSRF, "", "application/json", string(payload) + `{}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/rules", strings.NewReader(tc.body))
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("X-CSRF-Token", tc.token)
			req.Header.Set("Content-Type", tc.contentType)
			if tc.host != "" {
				req.Host = tc.host
			}
			resp, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.code {
				t.Fatalf("status %d, want %d", resp.StatusCode, tc.code)
			}
		})
	}
	if calls != 0 {
		t.Fatal("rejected request reached store")
	}
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/rules", bytes.NewReader(payload))
	req.Header.Set("Origin", server.URL)
	req.Header.Set("X-CSRF-Token", loaded.CSRF)
	req.Header.Set("Content-Type", "application/json")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || !c.Matches("включи ночник") || c.Matches("включи свет") {
		t.Fatal("rules did not update live")
	}
	req, _ = http.NewRequest(http.MethodPut, server.URL+"/api/rules", bytes.NewReader(payload))
	req.Header.Set("Origin", server.URL)
	req.Header.Set("X-CSRF-Token", loaded.CSRF)
	req.Header.Set("Content-Type", "application/json")
	resp, err = server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 409 {
		t.Fatal("stale revision was accepted")
	}
	for _, path := range []string{"/", "/assets/app.js", "/assets/style.css"} {
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || len(data) == 0 {
			t.Fatal("missing embedded asset", path)
		}
		if resp.Header.Get("Content-Security-Policy") == "" {
			t.Fatal("missing CSP")
		}
	}
}

func TestEditorRejectsPublicClientsAndHosts(t *testing.T) {
	c, _ := New(sample("http://192.168.1.11"), "")
	h, _ := NewEditor(c, EditorStore{})
	for _, tc := range []struct{ host, remote string }{
		{"192.168.1.23:8089", "8.8.8.8:5000"}, {"public.example:8089", "192.168.1.5:5000"},
	} {
		req := httptest.NewRequest("GET", "http://"+tc.host+"/", nil)
		req.RemoteAddr = tc.remote
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Fatal("non-LAN editor access accepted")
		}
	}
}
