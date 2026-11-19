package directha

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func climateRule(mode string, temp float64) Rule {
	return Rule{Phrases: []string{"кондиционер"}, Domain: "climate", Service: "set_temperature", EntityID: "climate.cabinet_ac", HVACMode: mode, Temperature: &temp, FanMode: "low"}
}
func TestClimateProfilesUseExactHARequests(t *testing.T) {
	for _, tc := range []struct {
		mode string
		temp float64
	}{{"heat", 26}, {"cool", 24}} {
		t.Run(tc.mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var data map[string]any
				if json.NewDecoder(r.Body).Decode(&data) != nil {
					t.Error("invalid body")
				}
				if data["entity_id"] != "climate.cabinet_ac" || r.Method != "POST" {
					t.Error("incorrect target")
				}
				if calls == 1 {
					if r.URL.Path != "/api/services/climate/set_temperature" || data["hvac_mode"] != tc.mode || data["temperature"] != tc.temp || len(data) != 3 {
						t.Error("incorrect climate profile", data)
					}
				} else if calls == 2 {
					if r.URL.Path != "/api/services/climate/set_fan_mode" || data["fan_mode"] != "low" || len(data) != 2 {
						t.Error("incorrect fan mode", data)
					}
				} else {
					t.Error("unexpected retry")
				}
				w.Write([]byte("[]"))
			}))
			defer server.Close()
			c, err := New(Config{URL: server.URL, Rules: []Rule{climateRule(tc.mode, tc.temp)}}, "test-token")
			if err != nil {
				t.Fatal(err)
			}
			answer, err := c.Handle(context.Background(), "кондиционер")
			if err != nil || answer != "" || calls != 2 {
				t.Fatal(answer, err, calls)
			}
		})
	}
}
func TestClimateTurnOffAndFailures(t *testing.T) {
	for _, failure := range []int{0, 1, 2} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls == failure {
				w.WriteHeader(500)
				return
			}
			w.Write([]byte("[]"))
		}))
		c, _ := New(Config{URL: server.URL, Rules: []Rule{climateRule("heat", 26)}}, "test-token")
		answer, err := c.Handle(context.Background(), "кондиционер")
		if err != nil {
			t.Fatal(err)
		}
		if failure == 1 && (calls != 1 || answer == "") {
			t.Fatal("failed first step must stop without retry")
		}
		if failure == 2 && (calls != 2 || !strings.Contains(answer, "вентилятор")) {
			t.Fatal("partial result must be reported")
		}
		server.Close()
	}
	r := Rule{Phrases: []string{"выключи кондиционер"}, Domain: "climate", Service: "turn_off", EntityID: "climate.cabinet_ac"}
	if validateAction(r) != nil || len(actionsFor(r)) != 1 || actionsFor(r)[0].Service != "turn_off" || len(actionsFor(r)[0].Data) != 1 {
		t.Fatal("off must not set temperature or fan")
	}
}
func TestClimateValidationAndSnapshotIsolation(t *testing.T) {
	for _, change := range []func(*Rule){
		func(r *Rule) { r.Temperature = nil }, func(r *Rule) { *r.Temperature = 99 }, func(r *Rule) { r.HVACMode = "dry" }, func(r *Rule) { r.Service = "turn_on" }, func(r *Rule) { r.Service = "turn_off" }, func(r *Rule) { r.Domain = "switch"; r.EntityID = "switch.test"; r.Service = "turn_on" }, func(r *Rule) { r.FanMode = "../unsafe" },
	} {
		r := climateRule("heat", 26)
		change(&r)
		if _, err := New(Config{URL: "http://192.168.1.11", Rules: []Rule{r}}, ""); err == nil {
			t.Fatal("invalid profile accepted", r)
		}
	}
	r := climateRule("heat", 26)
	c, err := New(Config{URL: "http://192.168.1.11", Rules: []Rule{r}}, "")
	if err != nil {
		t.Fatal(err)
	}
	*r.Temperature = 30
	copy := c.Rules()
	*copy[0].Temperature = 18
	if *c.Rules()[0].Temperature != 26 {
		t.Fatal("temperature pointer mutated active snapshot")
	}
}
