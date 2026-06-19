package secret

import "testing"

func TestLooksLikeSecret(t *testing.T) {
	cases := map[string]bool{
		"sk-ant-abc123def456ghi789jkl":             true,
		"ghp_0123456789abcdef0123456789abcdef0123": true,
		"hello":          false,
		"${MY_TOKEN}":    false, // already an env ref
		"$BRAVE_API_KEY": false,
	}
	for in, want := range cases {
		if LooksLikeSecret(in) != want {
			t.Errorf("LooksLikeSecret(%q)=%v want %v", in, LooksLikeSecret(in), want)
		}
	}
}

func TestEnvVarName(t *testing.T) {
	if got := EnvVarName("figma", "FIGMA_OAUTH_TOKEN"); got != "FIGMA_OAUTH_TOKEN" {
		t.Fatalf("got %q", got)
	}
	if got := EnvVarName("context7", ""); got != "CONTEXT7_TOKEN" {
		t.Fatalf("got %q", got)
	}
}
