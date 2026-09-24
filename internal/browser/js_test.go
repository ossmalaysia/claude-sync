package browser

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
)

// checkJS asks node to parse the snippet; it skips when node is absent.
func checkJS(t *testing.T, src string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	f := filepath.Join(t.TempDir(), "snippet.js")
	if err := os.WriteFile(f, []byte("const x = "+src+";\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, "--check", f).CombinedOutput(); err != nil {
		t.Fatalf("invalid JS: %v\n%s\n%s", err, out, src)
	}
}

func TestJSJSONEscapesHostileInput(t *testing.T) {
	body := []byte(`{"content":"</script>\"'` + "`${x}`" + `\n"}`)
	src := jsJSON("POST", "https://claude.ai/api/organizations/o/projects/p/docs", body)
	if !strings.Contains(src, jsString(string(body))) {
		t.Fatalf("body not embedded as a JS string literal:\n%s", src)
	}
	checkJS(t, src)
}

func TestJSJSONWithoutBody(t *testing.T) {
	src := jsJSON("GET", "https://claude.ai/api/organizations", nil)
	if !strings.Contains(src, "body:undefined") {
		t.Fatalf("GET must send no body:\n%s", src)
	}
	checkJS(t, src)
}

func TestJSDownloadAndUploadParse(t *testing.T) {
	checkJS(t, jsDownload("https://claude.ai/api/organizations/o/files/f/contents"))
	checkJS(t, jsUpload("https://claude.ai/api/organizations/o/projects/p/upload", `we"ird—name.pdf`, "application/pdf", "JVBERi0="))
}

// runJS evaluates snippet in node with location.host set to host and a fetch
// stub, and returns what the snippet resolved to plus whether fetch ran.
func runJS(t *testing.T, src, host string) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	prog := `globalThis.location={host:` + jsString(host) + `};let fetched=false;
globalThis.fetch=async()=>{fetched=true;return {status:200,text:async()=>"ok",arrayBuffer:async()=>new ArrayBuffer(0)}};
Promise.resolve(` + src + `).then(r=>console.log(JSON.stringify({host:r.host||"",status:r.status,fetched})));`
	f := filepath.Join(t.TempDir(), "run.js")
	if err := os.WriteFile(f, []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, f).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestSnippetsNeverFetchOffClaude(t *testing.T) {
	snippets := map[string]string{
		"json":     jsJSON("GET", "https://claude.ai/api/organizations", nil),
		"download": jsDownload("https://claude.ai/api/organizations/o/files/f/contents"),
		"upload":   jsUpload("https://claude.ai/api/organizations/o/projects/p/upload", "a.pdf", "application/pdf", "JVBERi0="),
	}
	for name, src := range snippets {
		if got := runJS(t, src, "login.microsoftonline.com"); got != `{"host":"login.microsoftonline.com","status":0,"fetched":false}` {
			t.Errorf("%s off claude.ai: %s", name, got)
		}
		if got := runJS(t, src, "claude.ai"); got != `{"host":"","status":200,"fetched":true}` {
			t.Errorf("%s on claude.ai: %s", name, got)
		}
	}
}

func TestCheckHostMapsToAuth(t *testing.T) {
	if err := checkHost(evalResult{Status: 200}); err != nil {
		t.Fatalf("on claude.ai: %v", err)
	}
	err := checkHost(evalResult{Host: "accounts.google.com"})
	if !errors.Is(err, ErrNotOnClaude) || !errors.Is(err, claudeapi.ErrAuth) || !strings.Contains(err.Error(), "accounts.google.com") {
		t.Fatalf("err=%v", err)
	}
}
