package directha

import (
	"errors"
	"math"
	"regexp"
)

var fanModePattern = regexp.MustCompile(`^[a-zA-Z0-9 _.-]{1,32}$`)

func validateAction(r Rule) error {
	if r.Domain == "climate" {
		if r.Service == "set_temperature" {
			if r.HVACMode != "heat" && r.HVACMode != "cool" {
				return errors.New("direct_ha: climate mode must be heat or cool")
			}
			if r.Temperature == nil || math.IsNaN(*r.Temperature) || math.IsInf(*r.Temperature, 0) || *r.Temperature < 5 || *r.Temperature > 35 {
				return errors.New("direct_ha: climate temperature must be between 5 and 35 C")
			}
			if r.FanMode != "" && !fanModePattern.MatchString(r.FanMode) {
				return errors.New("direct_ha: invalid fan mode")
			}
			return nil
		}
		if r.Service != "turn_off" {
			return errors.New("direct_ha: unsupported climate action")
		}
	} else {
		switch r.Domain {
		case "switch", "light", "fan", "input_boolean":
		default:
			return errors.New("direct_ha: unsupported domain")
		}
		if r.Service != "turn_on" && r.Service != "turn_off" {
			return errors.New("direct_ha: unsupported action")
		}
	}
	if r.Temperature != nil || r.HVACMode != "" || r.FanMode != "" {
		return errors.New("direct_ha: climate parameters require climate.set_temperature")
	}
	return nil
}

type serviceAction struct {
	Service string
	Data    map[string]any
}

func actionsFor(r Rule) []serviceAction {
	data := map[string]any{"entity_id": r.EntityID}
	if r.Domain == "climate" && r.Service == "set_temperature" {
		data["temperature"] = *r.Temperature
		data["hvac_mode"] = r.HVACMode
		actions := []serviceAction{{Service: "set_temperature", Data: data}}
		if r.FanMode != "" {
			actions = append(actions, serviceAction{Service: "set_fan_mode", Data: map[string]any{"entity_id": r.EntityID, "fan_mode": r.FanMode}})
		}
		return actions
	}
	return []serviceAction{{Service: r.Service, Data: data}}
}
