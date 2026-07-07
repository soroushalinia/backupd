package scheduler

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/robfig/cron/v3"
	"github.com/soroushalinia/backupd/internal/config"
	"github.com/soroushalinia/backupd/internal/engine"
	"github.com/soroushalinia/backupd/internal/state"
	"github.com/soroushalinia/backupd/internal/storage"
)

type Daemon struct {
	cron   *cron.Cron
	plans  []config.Plan
	store  *state.Store
	engine *engine.Engine
}

func NewDaemon(cfg *config.Config, store *state.Store) (*Daemon, error) {
	d := &Daemon{
		cron: cron.New(
			cron.WithParser(
				cron.NewParser(
					cron.Descriptor | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
				),
			),
		),
		plans:  cfg.Plans,
		store:  store,
		engine: engine.New(store),
	}

	for _, plan := range cfg.Plans {
		if plan.Schedule == "" {
			continue
		}
		p := plan
		spec := plan.Schedule
		if !strings.HasPrefix(spec, "@") {
			spec = "0 " + spec
		}
		_, err := d.cron.AddFunc(spec, func() {
			if err := d.runPlan(context.Background(), p); err != nil {
				log.Printf("scheduled backup %q failed: %v", p.Name, err)
			}
		})
		if err != nil {
			return nil, fmt.Errorf("plan %q schedule %q: %w", plan.Name, plan.Schedule, err)
		}
		log.Printf("scheduled plan %q: %s", plan.Name, spec)
	}

	return d, nil
}

func (d *Daemon) Start() {
	d.cron.Start()
}

func (d *Daemon) Stop() {
	ctx := d.cron.Stop()
	<-ctx.Done()
}

func (d *Daemon) Run(ctx context.Context) error {
	d.Start()
	<-ctx.Done()
	d.Stop()
	return nil
}

func (d *Daemon) runPlan(ctx context.Context, plan config.Plan) error {
	dest, err := storage.NewFromDest(plan.Destination)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	_, err = d.engine.Run(ctx, plan, dest)
	return err
}
