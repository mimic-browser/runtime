package optimize

import (
	"strings"

	"github.com/moreveal/mimic/internal/network"
)

func describeMatch(match network.ResourceMatch) string {
	var parts []string
	if match.URLGlob != "" {
		parts = append(parts, match.URLGlob)
	}
	for _, selector := range []struct {
		name   string
		values []string
	}{{"hosts", match.Hosts}, {"origins", match.Origins}, {"kinds", match.Kinds}, {"owners", match.Owners}, {"mechanisms", match.Mechanisms}} {
		if len(selector.values) > 0 {
			parts = append(parts, selector.name+": "+strings.Join(selector.values, ", "))
		}
	}
	if match.TopLevelSite != "" {
		parts = append(parts, "site: "+match.TopLevelSite)
	}
	if len(parts) == 0 {
		return "all matching resources"
	}
	return strings.Join(parts, "; ")
}
