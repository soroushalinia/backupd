package main

import (
	"strings"
	"testing"

	"github.com/soroushalinia/backupd/internal/config"
)

func TestSelectPlansNoArg(t *testing.T) {
	cfg := &config.Config{
		Plans: []config.Plan{
			{Name: "alpha"},
			{Name: "beta"},
		},
	}
	plans, err := selectPlans(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 || plans[0].Name != "alpha" || plans[1].Name != "beta" {
		t.Errorf("expected all plans, got %+v", plans)
	}
}

func TestSelectPlansNoArgNoPlans(t *testing.T) {
	_, err := selectPlans(&config.Config{}, nil)
	if err == nil {
		t.Fatal("expected error with no plans configured")
	}
}

func TestSelectPlansByName(t *testing.T) {
	cfg := &config.Config{
		Plans: []config.Plan{{Name: "alpha"}, {Name: "beta"}},
	}
	plans, err := selectPlans(cfg, []string{"beta"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].Name != "beta" {
		t.Errorf("expected only beta, got %+v", plans)
	}
}

func TestSelectPlansUnknownName(t *testing.T) {
	cfg := &config.Config{
		Plans: []config.Plan{{Name: "alpha"}, {Name: "beta"}},
	}
	_, err := selectPlans(cfg, []string{"gamma"})
	if err == nil {
		t.Fatal("expected error for unknown plan")
	}
	if !strings.Contains(err.Error(), "gamma") || !strings.Contains(err.Error(), "alpha") || !strings.Contains(err.Error(), "beta") {
		t.Errorf("error should mention the plan and the available plans, got: %v", err)
	}
}
