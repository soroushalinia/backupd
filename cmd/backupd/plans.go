package main

import (
	"fmt"
	"strings"

	"github.com/soroushalinia/backupd/internal/config"
)

// selectPlans resolves the optional plan-name argument to the plans a
// command should operate on. With no argument every configured plan is
// selected, matching the "one or all" behavior of `backupd status`. With an
// argument the matching plan is returned, or an error that lists the
// available plans so the user does not have to guess.
func selectPlans(cfg *config.Config, args []string) ([]config.Plan, error) {
	if len(args) == 0 {
		if len(cfg.Plans) == 0 {
			return nil, fmt.Errorf("no plans configured in the config file")
		}
		return cfg.Plans, nil
	}
	for _, p := range cfg.Plans {
		if p.Name == args[0] {
			return []config.Plan{p}, nil
		}
	}
	names := make([]string, 0, len(cfg.Plans))
	for _, p := range cfg.Plans {
		names = append(names, p.Name)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("plan %q not found (no plans configured)", args[0])
	}
	return nil, fmt.Errorf("plan %q not found (available plans: %s)", args[0], strings.Join(names, ", "))
}
