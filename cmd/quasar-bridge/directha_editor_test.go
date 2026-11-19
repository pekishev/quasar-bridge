package main

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"quasarbridge/internal/directha"
)

func editorFixture(t *testing.T) (*configurationControl, *directha.Controller, []byte) {
	t.Helper()
	raw, err := os.ReadFile("../../config.local.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["llm_model"] = "unrelated-setting"
	raw, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := readConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := directha.New(cfg.DirectHA, "never-expose-this-token")
	if err != nil {
		t.Fatal(err)
	}
	return &configurationControl{path: path, active: cfg}, controller, raw
}

func TestDirectHAEditorPersistsAndAppliesWithoutRestart(t *testing.T) {
	config, controller, original := editorFixture(t)
	doc, err := config.readDirectHARules()
	if err != nil {
		t.Fatal(err)
	}
	rules := []directha.Rule{{Phrases: []string{"тестовая команда"}, Domain: "switch", Service: "turn_on", EntityID: "switch.test"}}
	next, err := config.saveDirectHARules(controller, doc.Revision, rules)
	if err != nil {
		t.Fatal(err)
	}
	if !controller.Matches("тестовая команда") || controller.Matches("включи свет") {
		t.Fatal("runtime snapshot was not replaced")
	}
	if next.Revision == doc.Revision {
		t.Fatal("revision not updated")
	}
	backup, err := os.ReadFile(config.path + ".direct-ha-backup")
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatal("original configuration not backed up")
	}
	saved, err := readConfig(config.path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.LLMModel != "unrelated-setting" || len(saved.DirectHA.Rules) != 1 {
		t.Fatal("unrelated settings lost")
	}
	restarted, err := directha.New(saved.DirectHA, "")
	if err != nil || !restarted.Matches("тестовая команда") {
		t.Fatal("rules do not survive restart")
	}
	if requiresRestart(config.active, saved) {
		t.Fatal("editor change incorrectly requires restart")
	}
	if _, err := config.saveDirectHARules(controller, next.Revision, []directha.Rule{}); err != nil {
		t.Fatal(err)
	}
	saved, err = readConfig(config.path)
	if err != nil {
		t.Fatal(err)
	}
	if requiresRestart(config.active, saved) || controller.Matches("тестовая команда") {
		t.Fatal("empty rules not applied consistently")
	}
}

func TestDirectHAEditorRejectsInvalidStaleAndFailedWrites(t *testing.T) {
	config, controller, original := editorFixture(t)
	doc, err := config.readDirectHARules()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = config.saveDirectHARules(controller, "stale", doc.Rules); !errors.Is(err, directha.ErrConflict) {
		t.Fatal("missing conflict", err)
	}
	invalid := append(doc.Rules, doc.Rules[0])
	if _, err = config.saveDirectHARules(controller, doc.Revision, invalid); !errors.Is(err, directha.ErrInvalidRules) {
		t.Fatal("invalid rules accepted", err)
	}
	if err = os.Mkdir(config.path+".direct-ha-backup", 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = config.saveDirectHARules(controller, doc.Revision, []directha.Rule{}); err == nil {
		t.Fatal("failed backup was ignored")
	}
	current, _ := os.ReadFile(config.path)
	if !bytes.Equal(original, current) || !controller.Matches("включи свет") {
		t.Fatal("failed save changed file or live rules")
	}
}
