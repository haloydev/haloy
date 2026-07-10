package scripts

import (
	"bytes"
	"fmt"
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
	proxyPath    string
	unitDir      string
	logPath      string
	downloadPath string
}

func newUpgradeFixture(t *testing.T) upgradeFixture {
	return newUpgradeFixtureWithOptions(t, true)
}

// newUpgradeFixtureWithOptions creates the test environment. withProxy=false
// simulates a pre-split install (haloyd only) that triggers migration.
func newUpgradeFixtureWithOptions(t *testing.T, withProxy bool) upgradeFixture {
	t.Helper()

	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	unitDir := filepath.Join(root, "units")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}

	f := upgradeFixture{
		root:         root,
		binDir:       binDir,
		haloydPath:   filepath.Join(binDir, "haloyd"),
		proxyPath:    filepath.Join(binDir, "haloy-proxy"),
		unitDir:      unitDir,
		logPath:      filepath.Join(root, "commands.log"),
		downloadPath: filepath.Join(root, "download-path.log"),
	}
	if withProxy {
		writeHaloydBinary(t, f.proxyPath, "v1.0.0", 0o750)
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

    echo "$out" >> "$HALOY_DOWNLOAD_PATH_LOG"
    if [ "${HALOY_BAD_DOWNLOAD:-}" = "1" ]; then
        printf '%s\n' '#!/bin/sh' 'exit 1' > "$out"
    else
        printf '%s\n' \
            '#!/bin/sh' \
            'if [ "$1" = "version" ]; then' \
            '    if [ "$2" = "--json" ]; then' \
            '        if [ "${HALOY_MALFORMED_METADATA:-}" = "1" ]; then echo "{}"; exit 0; fi' \
            '        case "$0" in' \
            '            *haloy-proxy*) printf '\''{"version":"%s","proxy_generation":%s,"proxy_schema_version":%s}\\n'\'' "${HALOY_DOWNLOAD_VERSION:-v1.1.0}" "${HALOY_DOWNLOAD_PROXY_GENERATION:-1}" "${HALOY_DOWNLOAD_PROXY_SCHEMA:-1}" ;;' \
            '            *) printf '\''{"version":"%s","required_proxy_generation":%s,"required_proxy_schema_version":%s}\\n'\'' "${HALOY_DOWNLOAD_VERSION:-v1.1.0}" "${HALOY_REQUIRED_PROXY_GENERATION:-1}" "${HALOY_REQUIRED_PROXY_SCHEMA:-1}" ;;' \
            '        esac' \
            '        exit 0' \
            '    fi' \
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

func writeMetadataBinary(t *testing.T, path, version string, generation, schema int, proxy bool) {
	t.Helper()
	metadata := ""
	if proxy {
		metadata = fmt.Sprintf(`{"version":%q,"proxy_generation":%d,"proxy_schema_version":%d}`, version, generation, schema)
	} else {
		metadata = fmt.Sprintf(`{"version":%q,"required_proxy_generation":%d,"required_proxy_schema_version":%d}`, version, generation, schema)
	}
	body := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"version\" ]; then\n    if [ \"$2\" = \"--json\" ]; then\n        echo '%s'\n    else\n        echo %s\n    fi\n    exit 0\nfi\nexit 1\n", metadata, version)
	if err := os.WriteFile(path, []byte(body), 0o750); err != nil {
		t.Fatal(err)
	}
}

func runUpgradeScript(t *testing.T, f upgradeFixture, env ...string) (string, error) {
	t.Helper()
	return runUpgradeScriptArgs(t, f, nil, env...)
}

func runUpgradeScriptArgs(t *testing.T, f upgradeFixture, args []string, env ...string) (string, error) {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	cmdArgs := append([]string{filepath.Join(wd, "upgrade-server.sh")}, args...)
	cmd := exec.Command("sh", cmdArgs...)
	cmd.Env = append(
		os.Environ(),
		"PATH="+f.binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HALOY_UPGRADE_INIT_SYSTEM=systemd",
		"HALOY_UPGRADE_SLEEP_SECONDS=0",
		"HALOY_SYSTEMD_UNIT_DIR="+f.unitDir,
		"HALOY_INIT_D_DIR="+f.root,
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

func TestUpgradeServerDoesNotTouchProxyOnHaloydUpgrade(t *testing.T) {
	f := newUpgradeFixture(t)

	output, err := runUpgradeScript(t, f)
	if err != nil {
		t.Fatalf("expected successful upgrade, got %v:\n%s", err, output)
	}

	// The whole point of the split: upgrading haloyd never stops the proxy.
	log := readFile(t, f.logPath)
	if strings.Contains(log, "haloy-proxy stop") || strings.Contains(log, "stop haloy-proxy") {
		t.Fatalf("haloyd upgrade must not stop haloy-proxy, got log:\n%s", log)
	}
	if got := readFile(t, f.proxyPath); !strings.Contains(got, "v1.0.0") {
		t.Fatalf("haloy-proxy binary must be untouched, got:\n%s", got)
	}
	if !strings.Contains(output, "haloy-proxy is compatible; leaving it running") {
		t.Fatalf("expected compatibility decision in output, got:\n%s", output)
	}
}

func TestUpgradeServerUpgradesProxyFirstWhenGenerationIsRequired(t *testing.T) {
	f := newUpgradeFixture(t)

	output, err := runUpgradeScript(
		t, f,
		"HALOY_REQUIRED_PROXY_GENERATION=2",
		"HALOY_DOWNLOAD_PROXY_GENERATION=2",
	)
	if err != nil {
		t.Fatalf("expected compatibility upgrade to succeed, got %v:\n%s", err, output)
	}

	if got := readFile(t, f.proxyPath); !strings.Contains(got, "v1.1.0") {
		t.Fatalf("expected upgraded proxy, got:\n%s", got)
	}
	if got := readFile(t, f.haloydPath); !strings.Contains(got, "v1.1.0") {
		t.Fatalf("expected upgraded haloyd, got:\n%s", got)
	}

	log := readFile(t, f.logPath)
	startProxy := strings.Index(log, "systemctl start haloy-proxy")
	stopHaloyd := strings.Index(log, "systemctl stop haloyd")
	if startProxy < 0 || stopHaloyd < 0 || startProxy > stopHaloyd {
		t.Fatalf("expected proxy upgrade before haloyd stop, got log:\n%s", log)
	}
}

func TestUpgradeServerUpgradesProxyWhenSchemaIsRequired(t *testing.T) {
	f := newUpgradeFixture(t)

	output, err := runUpgradeScript(
		t, f,
		"HALOY_REQUIRED_PROXY_SCHEMA=2",
		"HALOY_DOWNLOAD_PROXY_SCHEMA=2",
	)
	if err != nil {
		t.Fatalf("expected schema-driven proxy upgrade to succeed, got %v:\n%s", err, output)
	}
	if !strings.Contains(output, "haloy-proxy is below the target server requirements") {
		t.Fatalf("expected proxy compatibility upgrade, got:\n%s", output)
	}
}

func TestUpgradeServerRepairsProxyWhenHaloydIsAlreadyCurrent(t *testing.T) {
	f := newUpgradeFixture(t)
	writeMetadataBinary(t, f.haloydPath, "v1.1.0", 2, 1, false)

	output, err := runUpgradeScript(
		t, f,
		"HALOY_DOWNLOAD_PROXY_GENERATION=2",
		"HALOY_OLD_VERSION=v1.1.0",
	)
	if err != nil {
		t.Fatalf("expected proxy repair to succeed, got %v:\n%s", err, output)
	}
	log := readFile(t, f.logPath)
	if !strings.Contains(log, "systemctl stop haloy-proxy") {
		t.Fatalf("expected proxy restart, got log:\n%s", log)
	}
	if strings.Contains(log, "systemctl stop haloyd") {
		t.Fatalf("current haloyd must not restart, got log:\n%s", log)
	}
}

func TestUpgradeServerDoesNotDowngradeNewerCompatibleProxy(t *testing.T) {
	f := newUpgradeFixture(t)
	writeMetadataBinary(t, f.proxyPath, "v1.0.0", 3, 2, true)

	output, err := runUpgradeScript(
		t, f,
		"HALOY_REQUIRED_PROXY_GENERATION=2",
		"HALOY_REQUIRED_PROXY_SCHEMA=2",
	)
	if err != nil {
		t.Fatalf("expected upgrade to succeed, got %v:\n%s", err, output)
	}
	log := readFile(t, f.logPath)
	if strings.Contains(log, "stop haloy-proxy") {
		t.Fatalf("newer compatible proxy must not restart, got log:\n%s", log)
	}
}

func TestUpgradeServerRejectsMalformedCompatibilityMetadataBeforeStoppingServices(t *testing.T) {
	f := newUpgradeFixture(t)

	output, err := runUpgradeScript(t, f, "HALOY_MALFORMED_METADATA=1")
	if err == nil {
		t.Fatal("expected malformed metadata to fail")
	}
	if !strings.Contains(output, "Could not read proxy compatibility metadata") {
		t.Fatalf("expected metadata error, got:\n%s", output)
	}
	if _, err := os.Stat(f.logPath); !os.IsNotExist(err) {
		t.Fatalf("services must not be touched, log stat err=%v", err)
	}
}

func TestUpgradeServerProxyComponent(t *testing.T) {
	f := newUpgradeFixture(t)

	output, err := runUpgradeScriptArgs(t, f, []string{"--component=proxy"})
	if err != nil {
		t.Fatalf("expected successful proxy upgrade, got %v:\n%s", err, output)
	}

	if got := readFile(t, f.proxyPath); !strings.Contains(got, "v1.1.0") {
		t.Fatalf("expected upgraded proxy binary, got:\n%s", got)
	}
	if got := readFile(t, f.haloydPath); !strings.Contains(got, "v1.0.0") {
		t.Fatalf("haloyd binary must be untouched on proxy upgrade, got:\n%s", got)
	}

	log := readFile(t, f.logPath)
	stopIndex := strings.Index(log, "systemctl stop haloy-proxy")
	startIndex := strings.Index(log, "systemctl start haloy-proxy")
	if stopIndex < 0 || startIndex < 0 || stopIndex > startIndex {
		t.Fatalf("expected proxy stop before start, got log:\n%s", log)
	}
	if strings.Contains(log, "systemctl stop haloyd") {
		t.Fatalf("proxy upgrade must not stop haloyd, got log:\n%s", log)
	}
}

func TestUpgradeServerReappliesSetcapOnProxyForOpenRC(t *testing.T) {
	f := newUpgradeFixture(t)

	output, err := runUpgradeScriptArgs(t, f, []string{"--component=proxy"}, "HALOY_UPGRADE_INIT_SYSTEM=openrc")
	if err != nil {
		t.Fatalf("expected successful OpenRC proxy upgrade, got %v:\n%s", err, output)
	}

	log := readFile(t, f.logPath)
	if !strings.Contains(log, "rc-service haloy-proxy stop") {
		t.Fatalf("expected OpenRC proxy stop, got log:\n%s", log)
	}
	if !strings.Contains(log, "setcap cap_net_bind_service=+ep "+f.proxyPath) {
		t.Fatalf("expected setcap on the proxy binary, got log:\n%s", log)
	}
}

func TestUpgradeServerMigratesPreSplitInstall(t *testing.T) {
	f := newUpgradeFixtureWithOptions(t, false)

	// Pre-split installs have a haloyd unit with the bind capability.
	oldUnit := "[Unit]\nDescription=Haloy Daemon\n[Service]\nAmbientCapabilities=CAP_NET_BIND_SERVICE\n"
	if err := os.WriteFile(filepath.Join(f.unitDir, "haloyd.service"), []byte(oldUnit), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := runUpgradeScript(t, f)
	if err != nil {
		t.Fatalf("expected successful migration, got %v:\n%s", err, output)
	}

	if got := readFile(t, f.proxyPath); !strings.Contains(got, "v1.1.0") {
		t.Fatalf("expected haloy-proxy to be installed, got:\n%s", got)
	}
	if got := readFile(t, f.haloydPath); !strings.Contains(got, "v1.1.0") {
		t.Fatalf("expected upgraded haloyd binary, got:\n%s", got)
	}

	// Ordering: stop old haloyd (releases 80/443), start proxy immediately,
	// then start new haloyd.
	log := readFile(t, f.logPath)
	stopHaloyd := strings.Index(log, "systemctl stop haloyd")
	startProxy := strings.Index(log, "systemctl start haloy-proxy")
	startHaloyd := strings.Index(log, "systemctl start haloyd")
	if stopHaloyd < 0 || startProxy < 0 || startHaloyd < 0 ||
		stopHaloyd > startProxy || startProxy > startHaloyd {
		t.Fatalf("expected stop haloyd -> start haloy-proxy -> start haloyd, got log:\n%s", log)
	}

	proxyUnit := readFile(t, filepath.Join(f.unitDir, "haloy-proxy.service"))
	if !strings.Contains(proxyUnit, "AmbientCapabilities=CAP_NET_BIND_SERVICE") {
		t.Fatalf("proxy unit must hold the bind capability:\n%s", proxyUnit)
	}

	haloydUnit := readFile(t, filepath.Join(f.unitDir, "haloyd.service"))
	if strings.Contains(haloydUnit, "AmbientCapabilities") {
		t.Fatalf("migrated haloyd unit must not keep the bind capability:\n%s", haloydUnit)
	}
	if !strings.Contains(haloydUnit, "Wants=network-online.target haloy-proxy.service") {
		t.Fatalf("migrated haloyd unit must want haloy-proxy:\n%s", haloydUnit)
	}
	if strings.Contains(haloydUnit, "Requires=haloy-proxy") {
		t.Fatalf("haloyd must not hard-require haloy-proxy (restart coupling):\n%s", haloydUnit)
	}

	if _, err := os.Stat(f.haloydPath + ".backup"); !os.IsNotExist(err) {
		t.Fatalf("binary backup should be removed after success, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(f.unitDir, "haloyd.service.backup")); !os.IsNotExist(err) {
		t.Fatalf("unit backup should be removed after success, stat err=%v", err)
	}
}

func TestUpgradeServerMigrationRollsBackWhenProxyStartFails(t *testing.T) {
	f := newUpgradeFixtureWithOptions(t, false)

	oldUnit := "[Unit]\nDescription=Haloy Daemon OLD\n[Service]\nAmbientCapabilities=CAP_NET_BIND_SERVICE\n"
	if err := os.WriteFile(filepath.Join(f.unitDir, "haloyd.service"), []byte(oldUnit), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := runUpgradeScript(t, f, "HALOY_FAIL_START=1")
	if err == nil {
		t.Fatal("expected migration to fail when service start fails")
	}
	if !strings.Contains(output, "Upgrade failed, attempting rollback") {
		t.Fatalf("expected rollback output, got:\n%s", output)
	}

	// haloyd binary and unit must be back to the pre-split state, and haloyd
	// must be restarted so the server keeps serving traffic the old way.
	if got := readFile(t, f.haloydPath); !strings.Contains(got, "v1.0.0") {
		t.Fatalf("expected rollback to keep old haloyd binary, got:\n%s", got)
	}
	if got := readFile(t, filepath.Join(f.unitDir, "haloyd.service")); got != oldUnit {
		t.Fatalf("expected haloyd unit restored, got:\n%s", got)
	}
	log := readFile(t, f.logPath)
	if !strings.Contains(log, "systemctl restart haloyd") {
		t.Fatalf("expected rollback restart of haloyd, got log:\n%s", log)
	}

	// The partial proxy install must be fully undone, otherwise the next run
	// would treat the server as already split and never start the proxy.
	if _, err := os.Stat(f.proxyPath); !os.IsNotExist(err) {
		t.Fatalf("expected rollback to remove the proxy binary, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(f.unitDir, "haloy-proxy.service")); !os.IsNotExist(err) {
		t.Fatalf("expected rollback to remove the proxy unit, stat err=%v", err)
	}
}

func TestUpgradeServerRetryAfterFailedMigrationMigratesAgain(t *testing.T) {
	f := newUpgradeFixtureWithOptions(t, false)

	oldUnit := "[Unit]\nDescription=Haloy Daemon OLD\n[Service]\nAmbientCapabilities=CAP_NET_BIND_SERVICE\n"
	if err := os.WriteFile(filepath.Join(f.unitDir, "haloyd.service"), []byte(oldUnit), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := runUpgradeScript(t, f, "HALOY_FAIL_START=1"); err == nil {
		t.Fatal("expected first migration attempt to fail")
	}

	// The retry must detect the pre-split install again and migrate.
	output, err := runUpgradeScript(t, f)
	if err != nil {
		t.Fatalf("expected retry to migrate successfully, got %v:\n%s", err, output)
	}
	if !strings.Contains(output, "migrating to the split proxy setup") {
		t.Fatalf("expected retry to run the migration path, got:\n%s", output)
	}
	if got := readFile(t, f.proxyPath); !strings.Contains(got, "v1.1.0") {
		t.Fatalf("expected haloy-proxy installed on retry, got:\n%s", got)
	}
	if got := readFile(t, f.haloydPath); !strings.Contains(got, "v1.1.0") {
		t.Fatalf("expected upgraded haloyd on retry, got:\n%s", got)
	}
	if got := readFile(t, filepath.Join(f.unitDir, "haloyd.service")); strings.Contains(got, "AmbientCapabilities") {
		t.Fatalf("expected rewritten haloyd unit on retry, got:\n%s", got)
	}
}

func TestUpgradeServerMigrationRollbackRestoresSetcapOnOpenRC(t *testing.T) {
	f := newUpgradeFixtureWithOptions(t, false)

	// OpenRC uses an init script instead of a systemd unit.
	oldInit := "#!/sbin/openrc-run\n# OLD pre-split haloyd script\n"
	initPath := filepath.Join(f.root, "haloyd")
	if err := os.WriteFile(initPath, []byte(oldInit), 0o755); err != nil {
		t.Fatal(err)
	}

	// HALOY_INACTIVE makes the post-upgrade is-active check fail, forcing a
	// rollback after the haloyd binary was already replaced.
	output, err := runUpgradeScript(t, f, "HALOY_UPGRADE_INIT_SYSTEM=openrc", "HALOY_INACTIVE=1")
	if err == nil {
		t.Fatal("expected migration to fail when the service is inactive")
	}
	if !strings.Contains(output, "Upgrade failed, attempting rollback") {
		t.Fatalf("expected rollback output, got:\n%s", output)
	}

	if got := readFile(t, f.haloydPath); !strings.Contains(got, "v1.0.0") {
		t.Fatalf("expected rollback to restore old haloyd binary, got:\n%s", got)
	}
	if got := readFile(t, initPath); got != oldInit {
		t.Fatalf("expected haloyd init script restored, got:\n%s", got)
	}

	// The restored pre-split haloyd binds 80/443 itself and cp -p drops the
	// capability xattr, so rollback must re-grant it.
	log := readFile(t, f.logPath)
	if !strings.Contains(log, "setcap cap_net_bind_service=+ep "+f.haloydPath) {
		t.Fatalf("expected setcap re-applied to restored haloyd, got log:\n%s", log)
	}
	if _, err := os.Stat(f.proxyPath); !os.IsNotExist(err) {
		t.Fatalf("expected rollback to remove the proxy binary, stat err=%v", err)
	}
}

func TestUpgradeServerMigrationRewritesHaloydInitScriptOnOpenRC(t *testing.T) {
	f := newUpgradeFixtureWithOptions(t, false)

	oldInit := "#!/sbin/openrc-run\n# OLD pre-split haloyd script\n"
	initPath := filepath.Join(f.root, "haloyd")
	if err := os.WriteFile(initPath, []byte(oldInit), 0o755); err != nil {
		t.Fatal(err)
	}

	output, err := runUpgradeScript(t, f, "HALOY_UPGRADE_INIT_SYSTEM=openrc")
	if err != nil {
		t.Fatalf("expected successful OpenRC migration, got %v:\n%s", err, output)
	}

	proxyInit := readFile(t, filepath.Join(f.root, "haloy-proxy"))
	if !strings.Contains(proxyInit, "openrc-run") || !strings.Contains(proxyInit, f.proxyPath) {
		t.Fatalf("expected haloy-proxy init script, got:\n%s", proxyInit)
	}

	haloydInit := readFile(t, initPath)
	if !strings.Contains(haloydInit, "after firewall haloy-proxy") {
		t.Fatalf("expected migrated haloyd init script ordered after haloy-proxy, got:\n%s", haloydInit)
	}

	log := readFile(t, f.logPath)
	if !strings.Contains(log, "setcap cap_net_bind_service=+ep "+f.proxyPath) {
		t.Fatalf("expected setcap on the proxy binary during migration, got log:\n%s", log)
	}
	if _, err := os.Stat(initPath + ".backup"); !os.IsNotExist(err) {
		t.Fatalf("init script backup should be removed after success, stat err=%v", err)
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
		"dist/haloy-proxy-linux-amd64",
		"dist/haloy-proxy-linux-arm64",
		"dist/checksums.txt",
	} {
		if !strings.Contains(workflow, current) {
			t.Fatalf("release workflow does not publish %q", current)
		}
	}
}
