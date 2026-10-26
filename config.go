package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type config struct {
	apiID       int
	apiHash     string
	phone       string
	password    string
	source      string
	target      string
	sessionPath string
	backfill    int
	keepAuthor  bool
	autoJoin    bool
	healthPort  string
}

func loadConfig() (config, error) {
	var c config

	apiID, err := strconv.Atoi(strings.TrimSpace(os.Getenv("TG_API_ID")))
	if err != nil || apiID <= 0 {
		return c, fmt.Errorf("TG_API_ID must be a positive integer")
	}
	c.apiID = apiID

	c.apiHash = strings.TrimSpace(os.Getenv("TG_API_HASH"))
	if c.apiHash == "" {
		return c, fmt.Errorf("TG_API_HASH is required")
	}

	c.phone = strings.TrimSpace(os.Getenv("TG_PHONE"))
	if c.phone == "" {
		return c, fmt.Errorf("TG_PHONE is required")
	}

	c.password = os.Getenv("TG_PASSWORD")

	c.source = normalizeChannelName(os.Getenv("TG_SOURCE"))
	if c.source == "" {
		return c, fmt.Errorf("TG_SOURCE is required")
	}

	c.target = normalizeChannelName(os.Getenv("TG_TARGET"))
	if c.target == "" {
		return c, fmt.Errorf("TG_TARGET is required")
	}

	c.sessionPath = strings.TrimSpace(os.Getenv("TG_SESSION"))
	if c.sessionPath == "" {
		c.sessionPath = "session.json"
	}

	c.backfill, err = envInt("TG_BACKFILL", 0)
	if err != nil {
		return c, err
	}
	if c.backfill < 0 {
		return c, fmt.Errorf("TG_BACKFILL cannot be negative")
	}

	c.keepAuthor = envBool("TG_KEEP_AUTHOR", false)
	c.autoJoin = envBool("TG_AUTO_JOIN", true)

	// PORT is set automatically by Render; TG_HEALTH_PORT is the manual override.
	c.healthPort = strings.TrimSpace(os.Getenv("PORT"))
	if c.healthPort == "" {
		c.healthPort = strings.TrimSpace(os.Getenv("TG_HEALTH_PORT"))
	}
	if c.healthPort == "" {
		c.healthPort = "8080"
	}

	return c, nil
}

func normalizeChannelName(value string) string {
	value = strings.TrimSpace(value)
	if i := strings.Index(value, "t.me/"); i >= 0 {
		value = value[i+len("t.me/"):]
	}
	value = strings.TrimPrefix(value, "s/")
	value = strings.TrimPrefix(value, "@")
	value = strings.Trim(value, "/")
	if i := strings.IndexAny(value, "/?#"); i >= 0 {
		value = value[:i]
	}
	return value
}

func envInt(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return value, nil
}

func envBool(name string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}
