package validation

import "testing"

func TestConfigurationValues(t *testing.T) {
	for value, want := range map[string]bool{
		"apps/jdk/25.0.4.1+1": true, "bin/java": true,
		"": false, ".": false, "../bin": false, "/bin": false,
		"bin//java": false, "bin/./java": false, "bin/": false,
		`bin\java`: false, "bin/$HOME": false, "bin/a b": false,
	} {
		if SafeRelativePath(value) != want {
			t.Errorf("SafeRelativePath(%q): want %v", value, want)
		}
	}
	for value, want := range map[string]bool{
		"JAVA_HOME": true, "_tool2": true,
		"": false, "2TOOL": false, "A-B": false, "A B": false, "A;B": false,
	} {
		if EnvironmentName(value) != want {
			t.Errorf("EnvironmentName(%q): want %v", value, want)
		}
	}
}
