# Scheduling

Plans with a `schedule` can be run two ways: the embedded daemon, or exported systemd units.

## Cron format

Standard 5-field cron:

```
minute hour day-of-month month day-of-week
```

```yaml
schedule: "0 3 * * *"   # every day at 03:00
schedule: "30 1 * * 1"  # every Monday at 01:30
schedule: "*/5 * * * *" # every 5 minutes
```

## Option 1: embedded daemon

```shell
backupd daemon
```

The daemon loads the config and runs each plan when its cron expression matches. Run it under
systemd or your favourite process supervisor.

## Option 2: systemd units

Generate a timer and service for one or all plans:

```shell
backupd export-systemd server
backupd export-systemd            # all plans
```

Install the generated units:

```shell
sudo cp *.service *.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now backupd-server.timer
```

Cron expressions are converted to systemd `OnCalendar` format (e.g. `0 3 * * *` becomes
`*-*-* 03:00:00`, and weekday-specific schedules map to `Mon`, `Tue`, etc.).
