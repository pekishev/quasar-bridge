package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"quasarbridge/internal/directha"
)

func directHARevision(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (c *configurationControl) readDirectHARules() (directha.RulesDocument, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, err := os.ReadFile(c.path)
	if err != nil {
		return directha.RulesDocument{}, err
	}
	cfg, err := parseConfig(raw, false)
	if err != nil {
		return directha.RulesDocument{}, err
	}
	return directha.RulesDocument{Revision: directHARevision(raw), Rules: cfg.DirectHA.Rules}, nil
}

func (c *configurationControl) saveDirectHARules(controller *directha.Controller, revision string, rules []directha.Rule) (directha.RulesDocument, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var empty directha.RulesDocument
	if c.restartPending() {
		return empty, directha.ErrConflict
	}
	raw, err := os.ReadFile(c.path)
	if err != nil {
		return empty, err
	}
	if revision != directHARevision(raw) {
		return empty, directha.ErrConflict
	}
	cfg, err := parseConfig(raw, false)
	if err != nil {
		return empty, err
	}
	if cfg.Mode != "local" || cfg.DirectHA.URL != c.active.DirectHA.URL {
		return empty, directha.ErrConflict
	}
	candidate, err := directha.New(directha.Config{URL: cfg.DirectHA.URL, Rules: rules}, "")
	if err != nil {
		return empty, directha.ErrInvalidRules
	}
	rules = candidate.Rules()
	var doc map[string]jsontext.Value
	if json.Unmarshal(raw, &doc) != nil || doc == nil {
		return empty, errors.New("invalid configuration document")
	}
	encoded, err := json.Marshal(rules)
	if err != nil {
		return empty, err
	}
	if err := setConfigValue(doc, "direct_ha.rules", encoded); err != nil {
		return empty, err
	}
	next, err := json.Marshal(doc, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return empty, err
	}
	next = append(next, '\n')
	if _, err := parseConfig(next, false); err != nil {
		return empty, directha.ErrInvalidRules
	}
	current, err := os.ReadFile(c.path)
	if err != nil || !bytes.Equal(current, raw) {
		return empty, directha.ErrConflict
	}
	if err := writeConfiguration(c.path+".direct-ha-backup", raw, 0600); err != nil {
		return empty, err
	}
	if err := writeConfiguration(c.path, next, 0600); err != nil {
		return empty, err
	}
	// Already validated above; publish one immutable snapshot for all sessions.
	if err := controller.ReplaceRules(rules); err != nil {
		return empty, err
	}
	c.active.DirectHA.Rules = controller.Rules()
	if len(c.active.DirectHA.Rules) == 0 {
		c.active.DirectHA.Rules = nil
	}
	return directha.RulesDocument{Revision: directHARevision(next), Rules: controller.Rules()}, nil
}

func startDirectHAEditor(ctx context.Context, address string, controller *directha.Controller, configuration *configurationControl) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return errors.New("editor listen address must include a port")
	}
	if host != "" && host != "localhost" {
		ip := net.ParseIP(strings.Trim(host, "[]"))
		if ip == nil || (!ip.IsPrivate() && !ip.IsLoopback() && !ip.IsUnspecified()) {
			return errors.New("editor must listen on a local interface")
		}
	}
	handler, err := directha.NewEditor(controller, directha.EditorStore{
		Read: configuration.readDirectHARules,
		Save: func(revision string, rules []directha.Rule) (directha.RulesDocument, error) {
			return configuration.saveDirectHARules(controller, revision, rules)
		},
	})
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("cannot listen for command editor: %w", err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 * 1024}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("command_editor_failed")
		}
	}()
	slog.Info("command_editor_listening", "address", address)
	return nil
}
