// Package directha executes only explicitly configured phrases against a LAN HA.
package directha

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"quasarbridge/internal/sound"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

type Rule struct {
	Phrases  []string `json:"phrases"`
	Domain   string   `json:"domain"`
	Service  string   `json:"service"`
	EntityID string   `json:"entity_id"`
}
type Config struct {
	URL   string `json:"url"`
	Rules []Rule `json:"rules"`
}
type Controller struct {
	base   string
	token  string
	rules  atomic.Pointer[ruleSnapshot]
	client *http.Client
}

type ruleSnapshot struct {
	lookup map[string]Rule
	rules  []Rule
}

func cloneRules(rules []Rule) []Rule {
	out := make([]Rule, len(rules))
	for i, r := range rules {
		out[i] = r
		out[i].Phrases = append([]string(nil), r.Phrases...)
	}
	return out
}

// Rules returns a copy so the editor cannot mutate an active voice request.
func (c *Controller) Rules() []Rule { return cloneRules(c.rules.Load().rules) }
func (c *Controller) ReplaceRules(rules []Rule) error {
	candidate, err := New(Config{URL: c.base, Rules: rules}, c.token)
	if err != nil {
		return err
	}
	candidate.client.CloseIdleConnections()
	c.rules.Store(candidate.rules.Load())
	return nil
}

var entityPattern = regexp.MustCompile(`^[a-z_]+\.[a-z0-9_]+$`)

// Normalize permits ASR capitalization, spacing and terminal punctuation, but
// does not remove negations, extra words, or clauses from a command.
func Normalize(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.ReplaceAll(text, "ё", "е")
	text = strings.TrimRight(text, " .,!?:;…")
	text = strings.Join(strings.Fields(text), " ")
	if strings.HasPrefix(text, "алиса,") {
		text = strings.TrimSpace(strings.TrimPrefix(text, "алиса,"))
	} else if strings.HasPrefix(text, "алиса ") {
		text = strings.TrimSpace(strings.TrimPrefix(text, "алиса "))
	}
	return text
}

func New(cfg Config, token string) (*Controller, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("direct_ha.url must be a LAN HTTP(S) origin")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || (!ip.IsPrivate() && !ip.IsLoopback()) {
		return nil, errors.New("direct_ha.url requires a private or loopback IP address")
	}
	c := &Controller{base: strings.TrimRight(cfg.URL, "/"), token: token, client: &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, TLSHandshakeTimeout: 3 * time.Second}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
	if len(cfg.Rules) > 256 {
		return nil, errors.New("direct_ha: at most 256 rules are supported")
	}
	snapshot := &ruleSnapshot{lookup: map[string]Rule{}, rules: cloneRules(cfg.Rules)}
	for _, r := range snapshot.rules {
		if len(r.Phrases) > 64 {
			return nil, errors.New("direct_ha: at most 64 phrases per rule are supported")
		}

		if !entityPattern.MatchString(r.EntityID) || !strings.HasPrefix(r.EntityID, r.Domain+".") {
			return nil, errors.New("direct_ha: invalid entity_id/domain")
		}
		switch r.Domain {
		case "switch", "light", "fan", "input_boolean":
		default:
			return nil, errors.New("direct_ha: unsupported domain")
		}
		if r.Service != "turn_on" && r.Service != "turn_off" {
			return nil, errors.New("direct_ha: only turn_on/turn_off are supported")
		}
		if len(r.Phrases) == 0 {
			return nil, errors.New("direct_ha: rule needs phrases")
		}
		for _, phrase := range r.Phrases {
			key := Normalize(phrase)
			if key == "" || len(phrase) > 512 {
				return nil, errors.New("direct_ha: empty phrase")
			}
			if _, exists := snapshot.lookup[key]; exists {
				return nil, errors.New("direct_ha: duplicate normalized phrase")
			}
			snapshot.lookup[key] = r
		}
	}
	c.rules.Store(snapshot)
	return c, nil
}
func (c *Controller) Matches(text string) bool {
	_, ok := c.rules.Load().lookup[Normalize(text)]
	return ok
}

func (c *Controller) request(ctx context.Context, method, path string, body io.Reader) error {
	if strings.TrimSpace(c.token) == "" {
		return errors.New("direct HA token is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return errors.New("cannot create direct HA request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return errors.New("direct HA request failed; not retried")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 65536))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("direct HA returned HTTP %d; not retried", resp.StatusCode)
	}
	return nil
}

// Check authenticates using a read-only endpoint and never calls a service.
func (c *Controller) Check(ctx context.Context) error {
	return c.request(ctx, http.MethodGet, "/api/", nil)
}
func (c *Controller) Handle(ctx context.Context, text string) (string, error) {
	r, ok := c.rules.Load().lookup[Normalize(text)]
	if !ok {
		return "", errors.New("phrase is not in the direct HA allowlist")
	}
	body, _ := json.Marshal(map[string]string{"entity_id": r.EntityID})
	if err := c.request(ctx, http.MethodPost, "/api/services/"+r.Domain+"/"+r.Service, bytes.NewReader(body)); err != nil {
		// A failed/ambiguous local operation must not be retried by cloud handlers.
		return "Не удалось подтвердить выполнение команды в Home Assistant.", nil
	}
	if sound.HasPlayer(ctx) {
		if err := sound.Play(ctx, sound.Request{Sound: "confirmation", Title: "direct-ha-confirmation"}); err != nil {
			return "", err
		}
	}
	return "", nil
}
