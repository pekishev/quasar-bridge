package directha

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"quasarbridge/internal/sound"
	"sync/atomic"
	"testing"
)

func sample(url string) Config {
	return Config{URL: url, Rules: []Rule{{Phrases: []string{"включи свет"}, Domain: "switch", Service: "turn_on", EntityID: "switch.cabinet"}}}
}
func TestExactAllowlist(t *testing.T) {
	c, err := New(sample("http://192.168.1.11:8123"), "secret")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"включи свет", "  ВКЛЮЧИ   СВЕТ! ", "Алиса, включи свет."} {
		if !c.Matches(s) {
			t.Errorf("missed %q", s)
		}
	}
	for _, s := range []string{"не включи свет", "включи свет и вентиляцию", "выключи свет", "включи музыку", "включи свет в детской", "повтори ответ", "начни новый разговор"} {
		if c.Matches(s) {
			t.Errorf("unsafe match %q", s)
		}
	}
}
func TestLocalOriginAndRules(t *testing.T) {
	for _, u := range []string{"https://example.com", "http://8.8.8.8", "file:///tmp/test", "http://user:password@192.168.1.11", "http://192.168.1.11/path", "http://192.168.1.11?token=x"} {
		if _, e := New(sample(u), "x"); e == nil {
			t.Errorf("accepted %s", u)
		}
	}
	cfg := sample("http://192.168.1.11")
	cfg.Rules = append(cfg.Rules, cfg.Rules[0])
	if _, e := New(cfg, "x"); e == nil {
		t.Error("duplicate accepted")
	}
	cfg = sample("http://192.168.1.11")
	cfg.Rules[0].EntityID = "light.other"
	if _, e := New(cfg, "x"); e == nil {
		t.Error("domain mismatch accepted")
	}
}
func TestOnlyMatchedServiceCalled(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/api/services/switch/turn_on" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("wrong request")
		}
		var body map[string]string
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["entity_id"] != "switch.cabinet" || len(body) != 1 {
			t.Error("wrong body")
		}
		w.Write([]byte("[]"))
	}))
	defer s.Close()
	c, e := New(sample(s.URL), "test-token")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Handle(context.Background(), "не включи свет"); e == nil {
		t.Error("unknown phrase accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("unknown phrase contacted HA")
	}
	var played atomic.Int32
	ctx := sound.WithPlayer(context.Background(), func(_ context.Context, r sound.Request) error {
		played.Add(1)
		if r.Sound != "success" || r.Title != "direct-ha-confirmation" || r.URL != "" {
			t.Error("wrong confirmation sound")
		}
		return nil
	})
	answer, e := c.Handle(ctx, "Алиса, включи свет!")
	if e != nil || answer != "" || calls.Load() != 1 || played.Load() != 1 {
		t.Fatal(answer, e, calls.Load(), played.Load())
	}
}
func TestNoRedirectRetryOrCloudFallback(t *testing.T) {
	var targetCalls, attempts atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	for _, status := range []int{http.StatusFound, http.StatusUnauthorized, http.StatusInternalServerError} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts.Add(1)
			w.Header().Set("Location", target.URL)
			w.WriteHeader(status)
		}))
		c, e := New(sample(s.URL), "secret")
		if e != nil {
			t.Fatal(e)
		}
		answer, e := c.Handle(context.Background(), "включи свет")
		s.Close()
		if e != nil || answer != "Не удалось подтвердить выполнение команды в Home Assistant." {
			t.Fatal("must return local failure without cloud fallback", answer, e)
		}
	}
	if attempts.Load() != 3 || targetCalls.Load() != 0 {
		t.Fatal("redirect or retry detected")
	}
}
func TestCheckIsReadOnly(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/" {
			t.Error("check performed action")
		}
		w.Write([]byte(`{"message":"API running."}`))
	}))
	defer s.Close()
	c, e := New(sample(s.URL), "x")
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Check(context.Background()); e != nil {
		t.Fatal(e)
	}
}
