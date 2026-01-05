package analytics

import (
	"testing"
	"time"
)

func TestConfigZeroValue(t *testing.T) {
	c := Config{}

	if err := c.validate(); err != nil {
		t.Error("validating the zero-value configuration failed:", err)
	}
}

func TestConfigInvalidInterval(t *testing.T) {
	c := Config{
		Interval: -1 * time.Second,
	}

	if err := c.validate(); err == nil {
		t.Error("no error returned when validating a malformed config")

	} else if e, ok := err.(ConfigError); !ok {
		t.Error("invalid error returned when checking a malformed config:", err)

	} else if e.Field != "Interval" || e.Value.(time.Duration) != (-1*time.Second) {
		t.Error("invalid field error reported:", e)
	}
}

func TestConfigInvalidBatchSize(t *testing.T) {
	c := Config{
		BatchSize: -1,
	}

	if err := c.validate(); err == nil {
		t.Error("no error returned when validating a malformed config")

	} else if e, ok := err.(ConfigError); !ok {
		t.Error("invalid error returned when checking a malformed config:", err)

	} else if e.Field != "BatchSize" || e.Value.(int) != -1 {
		t.Error("invalid field error reported:", e)
	}
}

func TestConfigMaxMessageBytesHardLimit(t *testing.T) {
	c := Config{
		MaxMessageBytes: 5000000, // 5 MB > 4 MB limit
	}

	if err := c.validate(); err == nil {
		t.Error("no error returned when validating a malformed config")

	} else if e, ok := err.(ConfigError); !ok {
		t.Error("invalid error returned when checking a malformed config:", err)

	} else if e.Field != "MaxMessageBytes" || e.Value.(int) != 5000000 {
		t.Error("invalid field error reported:", e)
	}
}

func TestConfigMaxBatchBytesHardLimit(t *testing.T) {
	c := Config{
		MaxBatchBytes: 5000000, // 5 MB > 4 MB limit
	}

	if err := c.validate(); err == nil {
		t.Error("no error returned when validating a malformed config")

	} else if e, ok := err.(ConfigError); !ok {
		t.Error("invalid error returned when checking a malformed config:", err)

	} else if e.Field != "MaxBatchBytes" || e.Value.(int) != 5000000 {
		t.Error("invalid field error reported:", e)
	}
}

func TestMakeConfigEnforcesHardLimits(t *testing.T) {
	c := makeConfig(Config{
		MaxMessageBytes: 5000000, // 5 MB > 4 MB limit
		MaxBatchBytes:   5000000, // 5 MB > 4 MB limit
	})

	if c.MaxMessageBytes != maxHardLimitBytes {
		t.Errorf("Expected MaxMessageBytes to be enforced to %d, got %d", maxHardLimitBytes, c.MaxMessageBytes)
	}
	if c.MaxBatchBytes != maxHardLimitBytes {
		t.Errorf("Expected MaxBatchBytes to be enforced to %d, got %d", maxHardLimitBytes, c.MaxBatchBytes)
	}
}
