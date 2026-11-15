package bridge

import (
	"context"
	"quasarbridge/internal/directha"
	"testing"
)

func TestLocalOnlyRoutingNeverUsesLLM(t *testing.T) {
	c, err := directha.New(directha.Config{URL: "http://192.168.1.11:8123", Rules: []directha.Rule{{Phrases: []string{"включи свет"}, Domain: "switch", Service: "turn_on", EntityID: "switch.cabinet"}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{LocalOnly: true, DirectHome: c, Home: c}
	for _, text := range []string{"включи музыку", "не включай свет", "повтори ответ", "начни новый разговор", "выключи свет", "включи свет и музыку"} {
		if d := e.Decide(context.Background(), text); d.Route != "yandex" {
			t.Errorf("%q routed to %s", text, d.Route)
		}
	}
	if d := e.Decide(context.Background(), "Алиса, включи свет!"); d.Route != "home" {
		t.Fatal(d)
	}
}
