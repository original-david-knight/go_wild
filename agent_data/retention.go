package data

import (
	"os"
	"strconv"
	"time"
)

const (
	defaultChatRetentionDays = 30
)

func chatRetentionDuration() time.Duration {
	return envDaysToDuration("GOWILD_CHAT_RETENTION_DAYS", defaultChatRetentionDays)
}

func envDaysToDuration(key string, defDays int) time.Duration {
	val := os.Getenv(key)
	if val == "" {
		if defDays <= 0 {
			return 0
		}
		return time.Duration(defDays) * 24 * time.Hour
	}
	days, err := strconv.Atoi(val)
	if err != nil || days <= 0 {
		return 0
	}
	return time.Duration(days) * 24 * time.Hour
}
