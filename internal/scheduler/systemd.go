package scheduler

import (
	"fmt"
	"strings"
)

type SystemdUnits struct {
	Service string
	Timer   string
}

func GenerateSystemd(planName, schedule, binaryPath, configPath string) (*SystemdUnits, error) {
	sysdTime, err := cronToSystemd(schedule)
	if err != nil {
		return nil, err
	}

	service := fmt.Sprintf(`[Unit]
Description=backupd - %s
Documentation=https://github.com/soroushalinia/backupd

[Service]
Type=oneshot
ExecStart=%s run %s --config %s
EnvironmentFile=-/etc/backupd.env
`, planName, binaryPath, planName, configPath)

	timer := fmt.Sprintf(`[Unit]
Description=backupd timer - %s

[Timer]
OnCalendar=%s
Persistent=true

[Install]
WantedBy=timers.target
`, planName, sysdTime)

	return &SystemdUnits{Service: service, Timer: timer}, nil
}

func cronToSystemd(cronExpr string) (string, error) {
	parts := strings.Fields(cronExpr)
	if len(parts) != 5 {
		return "", fmt.Errorf("expected 5-field cron expression, got %d", len(parts))
	}

	minute := parts[0]
	hour := parts[1]
	day := parts[2]
	month := parts[3]
	weekday := parts[4]

	datePart := "*" + mapDate(month, day)
	dowPart := mapDow(weekday)

	return fmt.Sprintf("%s %s %s:%s:00", dowPart, datePart, padZeros(hour, 2), padZeros(minute, 2)), nil
}

func mapDow(cron string) string {
	if cron == "*" || cron == "?" {
		return "*-*-*"
	}
	days := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	parts := strings.Split(cron, ",")
	for i, p := range parts {
		if p >= "0" && p <= "7" {
			parts[i] = days[int(p[0]-'0')]
		}
	}
	return strings.Join(parts, ",")
}

func padZeros(s string, n int) string {
	if len(s) < n {
		for _, c := range s {
			if c < '0' || c > '9' {
				return s
			}
		}
		for len(s) < n {
			s = "0" + s
		}
	}
	return s
}

func mapDate(month, day string) string {
	if month == "*" && day == "*" {
		return "-*-*"
	}
	if month == "*" {
		return fmt.Sprintf("-*-%s", day)
	}
	if day == "*" {
		return fmt.Sprintf("-%s-*", month)
	}
	return fmt.Sprintf("-%s-%s", month, day)
}
