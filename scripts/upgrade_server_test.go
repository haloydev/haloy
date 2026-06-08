package scripts

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type upgradeFixture struct {
	root         string
	binDir       string
	haloydPath   string
	logPath      string
	downloadPath string
}

func newUpgradeFixture(t *testing.T) upgradeFixture {
	t.Helper()

	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}

	f := upgradeFixture{
		root:         root,
		binDir:       binDir,
		haloydPath:   filepath.Join(binDir, "haloyd"),
		logPath:      filepath.Join(root, "commands.log"),
		downloadPath: filepath.Join(root, "download-path.log"),
	}

	writeExecutable(t, filepath.Join(binDir, "id"), `#!/bin/sh
if [ "$1" = "-u" ]; then
    echo "${HALOY_TEST_UID:-0}"
    exit 0
fi
exit 0
`)

	writeExecutable(t, filepath.Join(binDir, "uname"), `#!/bin/sh
case "$1" in
    -s) echo "${HALOY_TEST_UNAME_S:-Linux}" ;;
    -m) echo "${HALOY_TEST_UNAME_M:-x86_64}" ;;
    *) /usr/bin/uname "$@" ;;
esac
`)

	writeExecutable(t, filepath.Join(binDir, "curl"), `#!/bin/sh
out=""
url=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        -o)
            shift
            out="$1"
            ;;
        -*)
            ;;
        *)
            url="$1"
            ;;
    esac
    shift
done

if [ -n "$out" ]; then
    if [ "${HALOY_FAIL_DOWNLOAD:-}" = "1" ]; then
        exit 22
    fi

    echo "$out" > "$HALOY_DOWNLOAD_PATH_LOG"
    if [ "${HALOY_BAD_DOWNLOAD:-}" = "1" ]; then
        printf '%s\n' '#!/bin/sh' 'exit 1' > "$out"
    else
        printf '%s\n' \
            '#!/bin/sh' \
            'if [ "$1" = "version" ]; then' \
            "    echo ${HALOY_DOWNLOAD_VERSION:-v1.1.0}" \
            '    exit 0' \
            'fi' \
            'exit 1' > "$out"
    fi
    exit 0
fi

case "$url" in
    *releases/latest*) printf '{"tag_name":"%s"}\n' "${HALOY_LATEST_VERSION:-v1.1.0}" ;;
    *) printf '[{"tag_name":"%s"}]\n' "${HALOY_LATEST_VERSION:-v1.1.0}" ;;
esac
`)

	writeExecutable(t, filepath.Join(binDir, "systemctl"), `#!/bin/sh
echo "systemctl $*" >> "$HALOY_TEST_LOG"
if [ "$1" = "stop" ]; then
    if ! grep -q "$HALOY_OLD_VERSION" "$HALOYD_TARGET"; then
        echo "stop happened after binary replacement" >&2
        exit 45
    fi
fi
if [ "$1" = "start" ] && [ "${HALOY_FAIL_START:-}" = "1" ]; then
    exit 1
fi
if [ "$1" = "is-active" ]; then
    if [ "${HALOY_INACTIVE:-}" = "1" ]; then
        exit 3
    fi
    exit 0
fi
exit 0
`)

	writeExecutable(t, filepath.Join(binDir, "rc-service"), `#!/bin/sh
echo "rc-service $*" >> "$HALOY_TEST_LOG"
if [ "$2" = "stop" ]; then
    if ! grep -q "$HALOY_OLD_VERSION" "$HALOYD_TARGET"; then
        echo "stop happened after binary replacement" >&2
        exit 45
    fi
fi
if [ "$2" = "start" ] && [ "${HALOY_FAIL_START:-}" = "1" ]; then
    exit 1
fi
if [ "$2" = "status" ]; then
    if [ "${HALOY_INACTIVE:-}" = "1" ]; then
        exit 3
    fi
    exit 0
fi
exit 0
`)

	writeExecutable(t, filepath.Join(binDir, "setcap"), `#!/bin/sh
echo "setcap $*" >> "$HALOY_TEST_LOG"
if [ "${HALOY_FAIL_SETCAP:-}" = "1" ]; then
    exit 1
fi
exit 0
`)

	writeHaloydBinary(t, f.haloydPath, "v1.0.0", 0o750)
	return f
}

func writeExecutable(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeHaloydBinary(t *testing.T, path string, version string, mode os.FileMode) {
	t.Helper()
	body := "#!/bin/sh\nif [ \"$1\" = \"version\" ]; then\n    echo " + version + "\n    exit 0\nfi\nexit 1\n"
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func runUpgradeScript(t *testing.T, f upgradeFixture, env ...string) (string, error) {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("sh", filepath.Join(wd, "upgrade-server.sh"))
	cmd.Env = append(
		os.Environ(),
		"PATH="+f.binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HALOY_UPGRADE_INIT_SYSTEM=systemd",
		"HALOY_UPGRADE_SLEEP_SECONDS=0",
		"HALOYD_TARGET="+f.haloydPath,
		"HALOY_OLD_VERSION=v1.0.0",
		"HALOY_TEST_LOG="+f.logPath,
		"HALOY_DOWNLOAD_PATH_LOG="+f.downloadPath,
	)
	cmd.Env = append(cmd.Env, env...)

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err = cmd.Run()
	return output.String(), err
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUpgradeServerRequiresRoot(t *testing.T) {
	f := newUpgradeFixture(t)

	output, err := runUpgradeScript(t, f, "HALOY_TEST_UID=501")
	if err == nil {
		t.Fatal("expected script to fail for non-root user")
	}
	if !strings.Contains(output, "must be run as root") {
		t.Fatalf("expected root error, got:\n%s", output)
	}
}

func TestUpgradeServerRejectsUnsupportedArchitecture(t *testing.T) {
	f := newUpgradeFixture(t)

	output, err := runUpgradeScript(t, f, "HALOY_TEST_UNAME_M=mips")
	if err == nil {
		t.Fatal("expected script to fail for unsupported architecture")
	}
	if !strings.Contains(output, "Unsupported architecture: mips") {
		t.Fatalf("expected unsupported architecture error, got:\n%s", output)
	}
	if _, err := os.Stat(f.logPath); !os.IsNotExist(err) {
		t.Fatalf("service manager should not have been called, log stat err=%v", err)
	}
}

func TestUpgradeServerRejectsInvalidDownloadBeforeStoppingService(t *testing.T) {
	f := newUpgradeFixture(t)

	output, err := runUpgradeScript(t, f, "HALOY_BAD_DOWNLOAD=1")
	if err == nil {
		t.Fatal("expected script to fail for invalid downloaded binary")
	}
	if !strings.Contains(output, "Downloaded binary failed verification") {
		t.Fatalf("expected verification error, got:\n%s", output)
	}
	if _, err := os.Stat(f.logPath); !os.IsNotExist(err) {
		t.Fatalf("service manager should not have been called, log stat err=%v", err)
	}
	if got := readFile(t, f.haloydPath); !strings.Contains(got, "v1.0.0") {
		t.Fatalf("haloyd binary changed before service stop:\n%s", got)
	}
}

func TestUpgradeServerHandlesDownloadFailureBeforeStoppingService(t *testing.T) {
	f := newUpgradeFixture(t)

	output, err := runUpgradeScript(t, f, "HALOY_FAIL_DOWNLOAD=1")
	if err == nil {
		t.Fatal("expected script to fail when download fails")
	}
	if !strings.Contains(output, "Failed to download") {
		t.Fatalf("expected download error, got:\n%s", output)
	}
	if _, err := os.Stat(f.logPath); !os.IsNotExist(err) {
		t.Fatalf("service manager should not have been called, log stat err=%v", err)
	}
	if got := readFile(t, f.haloydPath); !strings.Contains(got, "v1.0.0") {
		t.Fatalf("haloyd binary changed before service stop:\n%s", got)
	}
}

func TestUpgradeServerReplacesBinaryAfterServiceStop(t *testing.T) {
	f := newUpgradeFixture(t)

	output, err := runUpgradeScript(t, f)
	if err != nil {
		t.Fatalf("expected successful upgrade, got %v:\n%s", err, output)
	}

	if got := readFile(t, f.haloydPath); !strings.Contains(got, "v1.1.0") {
		t.Fatalf("expected upgraded binary, got:\n%s", got)
	}

	info, err := os.Stat(f.haloydPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o750 {
		t.Fatalf("upgraded binary mode = %v, want 0750", got)
	}

	downloadPath := strings.TrimSpace(readFile(t, f.downloadPath))
	if filepath.Dir(downloadPath) != f.binDir {
		t.Fatalf("download path = %q, want directory %q", downloadPath, f.binDir)
	}

	log := readFile(t, f.logPath)
	stopIndex := strings.Index(log, "systemctl stop haloyd")
	startIndex := strings.Index(log, "systemctl start haloyd")
	if stopIndex < 0 || startIndex < 0 || stopIndex > startIndex {
		t.Fatalf("expected stop before start, got log:\n%s", log)
	}

	if _, err := os.Stat(f.haloydPath + ".backup"); !os.IsNotExist(err) {
		t.Fatalf("backup should be removed after success, stat err=%v", err)
	}
}

func TestUpgradeServerRollsBackWhenServiceStartFails(t *testing.T) {
	f := newUpgradeFixture(t)

	output, err := runUpgradeScript(t, f, "HALOY_FAIL_START=1")
	if err == nil {
		t.Fatal("expected script to fail when service start fails")
	}
	if !strings.Contains(output, "Upgrade failed, attempting rollback") {
		t.Fatalf("expected rollback output, got:\n%s", output)
	}
	if got := readFile(t, f.haloydPath); !strings.Contains(got, "v1.0.0") {
		t.Fatalf("expected rollback to restore old binary, got:\n%s", got)
	}

	log := readFile(t, f.logPath)
	if !strings.Contains(log, "systemctl restart haloyd") {
		t.Fatalf("expected rollback restart, got log:\n%s", log)
	}
}

func TestUpgradeServerReappliesSetcapForOpenRC(t *testing.T) {
	f := newUpgradeFixture(t)

	output, err := runUpgradeScript(t, f, "HALOY_UPGRADE_INIT_SYSTEM=openrc")
	if err != nil {
		t.Fatalf("expected successful OpenRC upgrade, got %v:\n%s", err, output)
	}

	log := readFile(t, f.logPath)
	if !strings.Contains(log, "rc-service haloyd stop") {
		t.Fatalf("expected OpenRC service stop, got log:\n%s", log)
	}
	if !strings.Contains(log, "setcap cap_net_bind_service=+ep "+f.haloydPath) {
		t.Fatalf("expected setcap to be reapplied, got log:\n%s", log)
	}
}

func TestReleaseWorkflowReferencesOnlyNativeServerArtifacts(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	workflowPath := filepath.Join(filepath.Dir(wd), ".github", "workflows", "release-tag.yaml")
	workflow := readFile(t, workflowPath)

	for _, legacy := range []string{
		"haloyadm",
		"haloy-haloyd",
		"docker/build-push-action",
		"docker/login-action",
	} {
		if strings.Contains(workflow, legacy) {
			t.Fatalf("release workflow still references legacy artifact %q", legacy)
		}
	}

	for _, current := range []string{
		"dist/haloyd-linux-amd64",
		"dist/haloyd-linux-arm64",
		"dist/checksums.txt",
	} {
		if !strings.Contains(workflow, current) {
			t.Fatalf("release workflow does not publish %q", current)
		}
	}
}
