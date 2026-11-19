package directha

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestEntityListIsReadOnlyAndRedactsAttributes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/states" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("incorrect entity request")
		}
		w.Write([]byte(`[{"entity_id":"sensor.temperature","state":"20","attributes":{"friendly_name":"Temperature"}},{"entity_id":"switch.cabinet","state":"on","attributes":{"friendly_name":"Кабинет","secret":"do-not-expose"}},{"entity_id":"light.night","state":"unavailable","attributes":{"friendly_name":"Ночник"}}]`))
	}))
	defer server.Close()
	c, err := New(sample(server.URL), "secret")
	if err != nil {
		t.Fatal(err)
	}
	entities, err := c.Entities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entities) != 2 || entities[0].ID != "switch.cabinet" || entities[1].State != "unavailable" {
		t.Fatal(entities)
	}
}

func TestRuleSnapshotsAreCopiedAndReplaceAtomically(t *testing.T) {
	cfg := sample("http://192.168.1.11")
	c, _ := New(cfg, "")
	cfg.Rules[0].Phrases[0] = "mutated"
	if !c.Matches("включи свет") {
		t.Fatal("caller changed snapshot")
	}
	rules := c.Rules()
	rules[0].Phrases[0] = "mutated"
	if !c.Matches("включи свет") {
		t.Fatal("returned slice changed snapshot")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			for j := 0; j < 100; j++ {
				c.Matches("включи свет")
				c.Rules()
			}
		})
	}
	for i := 0; i < 50; i++ {
		if err := c.ReplaceRules(sample("http://192.168.1.11").Rules); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	invalid := sample("http://192.168.1.11").Rules
	invalid[0].Service = "toggle"
	if c.ReplaceRules(invalid) == nil || !c.Matches("включи свет") {
		t.Fatal("invalid rules changed active snapshot")
	}
	long := sample("http://192.168.1.11").Rules
	long[0].Phrases = []string{strings.Repeat("a", 513)}
	if c.ReplaceRules(long) == nil {
		t.Fatal("unbounded phrase accepted")
	}
	if err := c.ReplaceRules([]Rule{}); err != nil || c.Matches("включи свет") {
		t.Fatal("empty rules did not disable matching")
	}
}
