package collector

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/aritaone/watchcat-agent/internal/config"
)

func customOnly(dir string) *config.Config {
	cfg := config.Default()
	cfg.Collectors = config.Collectors{CustomDir: dir}
	return cfg
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseCustom(t *testing.T) {
	samples, problems := parseCustom(`
# comment
backup_ok:1
  queue_depth : 42.5
url:http://x
bad-name:1
no_colon
not_number:abc
read_error:1
backup_ok:0
neg:-3e2
`)
	want := []customSample{{"backup_ok", 1}, {"queue_depth", 42.5}, {"neg", -300}}
	if len(samples) != len(want) {
		t.Fatalf("samples = %v, want %v", samples, want)
	}
	for i := range want {
		if samples[i] != want[i] {
			t.Errorf("sample %d = %v, want %v", i, samples[i], want[i])
		}
	}
	// url, bad-name, no_colon, not_number, read_error, duplicate backup_ok
	if len(problems) != 6 {
		t.Errorf("got %d problems, want 6: %v", len(problems), problems)
	}
}

func TestCustomReadsOnlyConfFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.conf"), "backup_ok:1\nqueue_depth:42\n")
	writeFile(t, filepath.Join(dir, "b.conf"), "backup_ok:0\r\n")
	writeFile(t, filepath.Join(dir, "ignored.txt"), "txt_metric:1\n")
	writeFile(t, filepath.Join(dir, ".hidden.conf"), "hidden_metric:1\n")
	if err := os.Mkdir(filepath.Join(dir, "sub.conf"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := certMetrics(t, customOnly(dir))

	ok := got["watchcat_custom_backup_ok"]
	if len(ok) != 2 {
		t.Fatalf("backup_ok series = %v, want 2", ok)
	}
	byFile := map[string]float64{}
	for _, s := range ok {
		byFile[s.labels["file"]] = s.value
	}
	if byFile["a.conf"] != 1 || byFile["b.conf"] != 0 {
		t.Errorf("backup_ok by file = %v", byFile)
	}
	if q := got["watchcat_custom_queue_depth"]; len(q) != 1 || q[0].value != 42 {
		t.Errorf("queue_depth = %v", q)
	}
	for _, name := range []string{"watchcat_custom_txt_metric", "watchcat_custom_hidden_metric"} {
		if _, found := got[name]; found {
			t.Errorf("%s should not be exported", name)
		}
	}
	if e := got["watchcat_custom_read_error"]; len(e) != 2 || e[0].value != 0 || e[1].value != 0 {
		t.Errorf("read_error = %v, want two 0s", e)
	}
	if e := got["watchcat_custom_dir_error"]; len(e) != 1 || e[0].value != 0 {
		t.Errorf("dir_error = %v, want a single 0", e)
	}
}

func TestCustomMissingDir(t *testing.T) {
	got := certMetrics(t, customOnly(filepath.Join(t.TempDir(), "missing")))
	if e := got["watchcat_custom_dir_error"]; len(e) != 1 || e[0].value != 1 {
		t.Errorf("dir_error = %v, want a single 1", e)
	}
	for _, s := range got["watchcat_scrape_success"] {
		if s.labels["collector"] == "custom" && s.value != 1 {
			t.Error("a missing folder must not fail the collector")
		}
	}
}

func TestCustomUnreadableFile(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("file permissions are not enforced here")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.conf")
	writeFile(t, path, "x:1\n")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}

	got := certMetrics(t, customOnly(dir))
	if e := got["watchcat_custom_read_error"]; len(e) != 1 || e[0].value != 1 || e[0].labels["file"] != "secret.conf" {
		t.Errorf("read_error = %v, want a single 1 for secret.conf", e)
	}
}

func TestCustomDisabledWithoutDir(t *testing.T) {
	c, err := New(customOnly(""), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range c.Enabled() {
		if name == "custom" {
			t.Fatal("custom collector should be off when custom_dir is empty")
		}
	}
}
