package progressui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRunLogsPlainOutput(t *testing.T) {
	var out bytes.Buffer
	err := Run(context.Background(), &out, "Installing Node.js", func(report Reporter) error {
		report("Downloading artifact", 1, 2)
		report("Verifying checksum", 0, 0)
		return errors.New("checksum mismatch")
	})
	if err == nil || !strings.Contains(out.String(), "Installing Node.js") ||
		!strings.Contains(out.String(), "Downloading artifact") ||
		!strings.Contains(out.String(), "50.00%") ||
		!strings.Contains(out.String(), "Verifying checksum") ||
		!strings.Contains(out.String(), "Failed: checksum mismatch") {
		t.Fatalf("output=%q err=%v", out.String(), err)
	}
}

func TestRunMarksSuccessfulCompletion(t *testing.T) {
	var out bytes.Buffer
	if err := Run(context.Background(), &out, "Installing Node.js", func(Reporter) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "✓ Complete") {
		t.Fatalf("output=%q", out.String())
	}
}
