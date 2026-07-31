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
		cron:   cron.New(cronParser()),
		plans:  cfg.Plans,
		store:  store,
		engine: engine.New(store),
	}

	for _, plan := range cfg.Plans {
		if plan.Schedule == "" {
			continue
		}
		p := plan
		spec := normalizeSpec(plan.Schedule)
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

func cronParser() cron.Option {
	return cron.WithParser(
		cron.NewParser(
			cron.Descriptor | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
		),
	)
}

// normalizeSpec accepts both 4-field hour-level specs ("3 * * *") and
// 5-field minute-level specs ("*/5 * * * *"); the latter are already
// supported by the parser and must not be modified.
func normalizeSpec(spec string) string {
	if strings.HasPrefix(spec, "@") {
		return spec
	}
	if len(strings.Fields(spec)) == 4 {
		return "0 " + spec
	}
	return spec
}

// ValidateSchedule reports whether a plan's schedule is a valid cron
// expression, so misconfigured schedules can be caught by `backupd check`.
func ValidateSchedule(spec string) error {
	c := cron.New(cronParser())
	_, err := c.AddFunc(normalizeSpec(spec), func() {})
	return err
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
