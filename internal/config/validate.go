package config

import "fmt"

func (c *Config) Validate() error {
	if len(c.Plans) == 0 {
		return fmt.Errorf("at least one plan is required")
	}
	for i, plan := range c.Plans {
		if err := plan.Validate(); err != nil {
			return fmt.Errorf("plan %d (%q): %w", i, plan.Name, err)
		}
	}
	return nil
}

func (p *Plan) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("plan name is required")
	}
	if len(p.Sources) == 0 {
		return fmt.Errorf("at least one source is required")
	}
	for i, src := range p.Sources {
		if err := src.Validate(); err != nil {
			return fmt.Errorf("source %d: %w", i, err)
		}
	}
	if err := p.Destination.Validate(); err != nil {
		return fmt.Errorf("destination: %w", err)
	}
	return nil
}

func (s *Source) Validate() error {
	switch s.Type {
	case "file":
		if s.Path == "" {
			return fmt.Errorf("path is required for file source")
		}
	case "database":
		if s.Adapter == "" {
			return fmt.Errorf("adapter is required for database source")
		}
		if s.DSN == "" {
			return fmt.Errorf("dsn is required for database source")
		}
	case "docker":
		if s.Volume == "" {
			return fmt.Errorf("volume is required for docker source")
		}
	case "kubernetes":
		if s.PVC == "" {
			return fmt.Errorf("pvc is required for kubernetes source")
		}
	default:
		return fmt.Errorf("unknown source type: %q (valid: file, database, docker, kubernetes)", s.Type)
	}
	return nil
}

func (d *Destination) Validate() error {
	if d.Type != "s3" {
		return fmt.Errorf("only s3 destination is supported")
	}
	if d.Bucket == "" {
		return fmt.Errorf("bucket is required")
	}
	if d.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}
	return nil
}
