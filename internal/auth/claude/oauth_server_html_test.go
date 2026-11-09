package claude

import (
	"strings"
	"testing"
)

func TestSuccessHTMLRejectsScriptLinksAndEscapesAttributes(t *testing.T) {
	server := &OAuthServer{}
	for _, setup := range []bool{false, true} {
		for _, unsafe := range []string{`javascript:alert(1)`, `data:text/html,<script>alert(1)</script>`, `//attacker.invalid`} {
			page := server.generateSuccessHTML(setup, unsafe)
			if strings.Contains(page, unsafe) || !strings.Contains(page, `https://console.anthropic.com/`) {
				t.Errorf("unsafe platform URL %q accepted", unsafe)
			}
		}
		page := server.generateSuccessHTML(setup, `https://example.invalid/?q=" onclick="alert(1)`)
		if strings.Contains(page, `q=" onclick=`) || !strings.Contains(page, `q=&#34; onclick=&#34;alert(1)`) {
			t.Error("platform link must be HTML escaped")
		}
	}
}
