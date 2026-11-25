package gitenv

import (
	"os"
	"strings"
	"testing"
)

func TestEnvDropsGitConfigRedirects(t *testing.T) {
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.worktree")
	t.Setenv("GIT_CONFIG_VALUE_0", "/tmp/elsewhere")
	t.Setenv("GIT_OPTIONAL_LOCKS", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

	values := map[string]string{}
	for _, kv := range Env() {
		key, value, ok := strings.Cut(kv, "=")
		if ok {
			values[key] = value
		}
	}
	for _, key := range []string{"GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "GIT_OPTIONAL_LOCKS"} {
		if value, ok := values[key]; ok {
			t.Errorf("%s survived as %q", key, value)
		}
	}
	for _, key := range []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM"} {
		if value := values[key]; value != os.DevNull {
			t.Errorf("%s = %q, want %q", key, value, os.DevNull)
		}
	}
}

func TestDropEnvIsCaseInsensitiveOnWindows(t *testing.T) {
	for _, key := range []string{
		"git_config_count",
		"Git_Config_Key_0",
		"git_config_value_0",
		"git_optional_locks",
	} {
		if !dropEnvForOS(key, "windows") {
			t.Errorf("%s was not dropped on Windows", key)
		}
	}
}
