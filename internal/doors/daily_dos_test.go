package doors

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDailyDOSBox(t *testing.T) {
	if _, err := exec.LookPath("dosbox-x"); err != nil {
		t.Skip("no dosbox-x")
	}
	dir := t.TempDir()
	out, err := RunDaily(context.Background(), Door{Name: "D", Kind: "dosbox", DOSBoxDir: dir, Daily: "ECHO new day > DAILY.TXT"})
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.EqualFold(e.Name(), "DAILY.TXT") {
			data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			if strings.Contains(string(data), "new day") {
				return
			}
		}
	}
	t.Fatalf("the DOS command didn't run; dir: %v", entries)
}
