package config

import "time"

type Config struct {
	Plans []Plan `mapstructure:"plans" yaml:"plans"`
}

type Plan struct {
	Name        string        `mapstructure:"name" yaml:"name"`
	Schedule    string        `mapstructure:"schedule" yaml:"schedule"`
	Sources     []Source      `mapstructure:"sources" yaml:"sources"`
	Destination Destination   `mapstructure:"destination" yaml:"destination"`
	Encryption  *Encryption   `mapstructure:"encryption" yaml:"encryption"`
	Retention   *Retention    `mapstructure:"retention" yaml:"retention"`
	Tags        map[string]string `mapstructure:"tags" yaml:"tags"`
	Hooks       *Hooks        `mapstructure:"hooks" yaml:"hooks"`
}

type Source struct {
	Type     string   `mapstructure:"type" yaml:"type"`
	Path     string   `mapstructure:"path" yaml:"path"`
	Exclude  []string `mapstructure:"exclude" yaml:"exclude"`
	Adapter  string   `mapstructure:"adapter" yaml:"adapter"`
	DSN      string   `mapstructure:"dsn" yaml:"dsn"`
	DumpTool string   `mapstructure:"dump-tool" yaml:"dump-tool"`
	Volume   string   `mapstructure:"volume" yaml:"volume"`
	PVC      string   `mapstructure:"pvc" yaml:"pvc"`
	Snapshot bool     `mapstructure:"snapshot" yaml:"snapshot"`
}

type Destination struct {
	Type         string `mapstructure:"type" yaml:"type"`
	Bucket       string `mapstructure:"bucket" yaml:"bucket"`
	Prefix       string `mapstructure:"prefix" yaml:"prefix"`
	Endpoint     string `mapstructure:"endpoint" yaml:"endpoint"`
	Region       string `mapstructure:"region" yaml:"region"`
	AccessKey    string `mapstructure:"access-key" yaml:"access-key"`
	SecretKey    string `mapstructure:"secret-key" yaml:"secret-key"`
	StorageClass string `mapstructure:"storage-class" yaml:"storage-class"`
	Secure       *bool  `mapstructure:"secure" yaml:"secure"`
}

type Encryption struct {
	Passphrase string `mapstructure:"passphrase" yaml:"passphrase"`
}

type Retention struct {
	KeepLast    int `mapstructure:"keep-last" yaml:"keep-last"`
	KeepDaily   int `mapstructure:"keep-daily" yaml:"keep-daily"`
	KeepWeekly  int `mapstructure:"keep-weekly" yaml:"keep-weekly"`
	KeepMonthly int `mapstructure:"keep-monthly" yaml:"keep-monthly"`
}

type Hooks struct {
	PreBackup []string `mapstructure:"pre-backup" yaml:"pre-backup"`
	PostBackup []string `mapstructure:"post-backup" yaml:"post-backup"`
	OnFailure []string `mapstructure:"on-failure" yaml:"on-failure"`
}

type Snapshot struct {
	ID        string    `json:"id"`
	Plan      string    `json:"plan"`
	Timestamp time.Time `json:"timestamp"`
	Sources   []string  `json:"sources"`
	Size      int64     `json:"size"`
	Tags      map[string]string `json:"tags"`
}
