package directha

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
)

type Entity struct {
	ID    string `json:"entity_id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// Entities exposes only the names and states needed by the editor, never HA
// credentials or arbitrary attributes. It performs no device actions.
func (c *Controller) Entities(ctx context.Context) ([]Entity, error) {
	if strings.TrimSpace(c.token) == "" {
		return nil, errors.New("HA token is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/states", nil)
	if err != nil {
		return nil, errors.New("cannot create HA entity request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, errors.New("HA entity request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("HA entity request was rejected")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024+1))
	if err != nil || len(data) > 4*1024*1024 {
		return nil, errors.New("HA entity response is too large or incomplete")
	}
	var states []struct {
		ID         string `json:"entity_id"`
		State      string `json:"state"`
		Attributes struct {
			Name string `json:"friendly_name"`
		} `json:"attributes"`
	}
	if json.Unmarshal(data, &states) != nil {
		return nil, errors.New("HA entity response is invalid")
	}
	entities := make([]Entity, 0)
	for _, s := range states {
		domain, _, _ := strings.Cut(s.ID, ".")
		if !entityPattern.MatchString(s.ID) {
			continue
		}
		switch domain {
		case "switch", "light", "fan", "input_boolean":
		default:
			continue
		}
		name := s.Attributes.Name
		if name == "" {
			name = s.ID
		}
		entities = append(entities, Entity{ID: s.ID, Name: name, State: s.State})
	}
	sort.Slice(entities, func(i, j int) bool {
		if entities[i].Name == entities[j].Name {
			return entities[i].ID < entities[j].ID
		}
		return entities[i].Name < entities[j].Name
	})
	return entities, nil
}
