package shellenv

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestConfigureWritesAndRemovesCatalogEnvironment(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".yapp"), 0o700); err != nil {
		t.Fatal(err)
	}
	startup := beginMarker + "\nsource ~/.yapp/yapp-env.sh\n" + endMarker + "\n"
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte(startup), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Configure(home, "bash", map[string]string{"JAVA_HOME": "apps/jdk/25"}, []string{"bin"}, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".bashrc"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), beginMarker) || strings.Contains(string(data), endMarker) ||
		strings.Count(string(data), `. "${HOME}/.yapp/yapp-env.sh"`) != 1 {
		t.Fatalf("unexpected shell setup: %s", data)
	}
	envPath := filepath.Join(home, ".yapp", "yapp-env.sh")
	env, err := os.ReadFile(envPath)
	if err != nil || !strings.Contains(string(env), `export JAVA_HOME="${YAPP_HOME}/apps/jdk/25"`) {
		t.Fatalf("JAVA_HOME missing from generated environment: %s (%v)", env, err)
	}
	if err := Configure(home, "bash", nil, []string{"bin"}, nil); err != nil {
		t.Fatal(err)
	}
	env, err = os.ReadFile(envPath)
	if err != nil || strings.Contains(string(env), "JAVA_HOME") {
		t.Fatalf("JAVA_HOME remained after JDK removal: %s (%v)", env, err)
	}
}

func TestRenderEnvironment(t *testing.T) {
	var output bytes.Buffer
	if err := Render(&output, map[string]string{"JAVA_HOME": "apps/jdk/25", "M2_HOME": "apps/maven/3"}, []string{"bin"}); err != nil {
		t.Fatal(err)
	}
	result := output.String()
	for _, want := range []string{`export YAPP_HOME="${HOME}/.yapp"`, `export JAVA_HOME="${YAPP_HOME}/apps/jdk/25"`, `export M2_HOME="${YAPP_HOME}/apps/maven/3"`, `${YAPP_HOME}/bin`} {
		if !strings.Contains(result, want) {
			t.Fatalf("shell environment missing %q: %s", want, result)
		}
	}
}

func TestStartupFileMatchesHomebrewChoices(t *testing.T) {
	for _, test := range []struct {
		shell, goos, want string
	}{
		{"bash", "linux", ".bashrc"},
		{"bash", "darwin", ".bash_profile"},
		{"zsh", "linux", ".zshrc"},
		{"zsh", "darwin", ".zprofile"},
	} {
		if got := startupFile(test.shell, test.goos); got != test.want {
			t.Errorf("startupFile(%q, %q) = %q, want %q", test.shell, test.goos, got, test.want)
		}
	}
}

func TestConcurrentEnvironmentWrites(t *testing.T) {
	home := t.TempDir()
	var writers sync.WaitGroup
	errs := make(chan error, 8)
	wants := make(map[string]bool)
	for i := 0; i < cap(errs); i++ {
		data := []byte("JAVA_HOME=" + string(rune('A'+i)))
		wants[string(data)] = true
		writers.Add(1)
		go func(data []byte) {
			defer writers.Done()
			errs <- writeEnvironment(home, data, func(string) {})
		}(data)
	}
	writers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(filepath.Join(home, "yapp-env.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !wants[string(got)] {
		t.Fatalf("environment file has partial or unexpected contents: %q", got)
	}
}

func TestEnvironmentWriterReportsLockWait(t *testing.T) {
	home := t.TempDir()
	lockPath := filepath.Join(home, ".yapp-env.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		lock.Close()
		t.Fatal(err)
	}

	messages := make(chan string, 1)
	finished := make(chan error, 1)
	go func() {
		finished <- writeEnvironment(home, []byte("ready"), func(message string) { messages <- message })
	}()
	select {
	case message := <-messages:
		if !strings.Contains(message, "Waiting") {
			t.Fatalf("unexpected wait message: %q", message)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("writer did not report that it was waiting")
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		lock.Close()
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("writer did not continue after the lock was released")
	}
}
