import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { test } from "node:test";

const image = process.env.SPECGATE_TEST_APPLIANCE_IMAGE;
const source = fileURLToPath(new URL("./", import.meta.url));

test("appliance supervisor protects state and preserves lifecycle diagnostics", { skip: !image }, () => {
  const result = spawnSync("docker", ["run", "--rm", "--network", "none", "--entrypoint", "bash",
    "--mount", `type=bind,source=${source},target=/source,readonly`, image, "-ceu", `
    export SETTINGS_ENCRYPTION_KEY=$(printf '%064d' 0)
    mkdir -p /data/diagnostics
    printf 'legacy diagnostic\\n' >/data/diagnostics/legacy.json
    chown specgate:specgate /data/diagnostics /data/diagnostics/legacy.json
    chmod 0777 /data/diagnostics
    chmod 0666 /data/diagnostics/legacy.json
    printf 'private diagnostic\\n' >/data/diagnostics/private.json
    chmod 0600 /data/diagnostics/private.json
    exec() { [[ "$1" = /init ]]; }
    source /source/entrypoint.sh
    unset -f exec
    test "$(cat /data/diagnostics/legacy.json)" = 'legacy diagnostic'
    test "$(stat -c %u:%a /data/diagnostics/legacy.json)" = 0:644
    test "$(stat -c %a /data/diagnostics/private.json)" = 600
    for dir in /run/specgate /run/specgate/components /run/specgate/restarts /data/diagnostics; do
    test "$(stat -c %u "$dir")" = 0 || { echo "component owns supervisor state: $dir"; exit 1; }
      if /command/s6-setuidgid specgate touch "$dir/component-write"; then exit 1; fi
    done
    printf '#!/bin/sh\nprintf true\\n\n' >/command/s6-svstat
    chmod +x /command/s6-svstat
    printf 'a[$(touch /tmp/arithmetic-executed)0]\\n' >/run/specgate/restarts/nginx
    if bash /source/service-finish.sh nginx 1 0; then exit 1; fi
    test ! -e /tmp/arithmetic-executed
    for invalid in indirect 999999999999999999999999 '1\n2'; do
      printf '%b\\n' "$invalid" >/run/specgate/restarts/nginx
      if bash /source/service-finish.sh nginx 1 0; then exit 1; fi
    done
    printf '0001\\n' >/run/specgate/restarts/nginx
    bash /source/service-finish.sh nginx 1 0
    test "$(cat /run/specgate/restarts/nginx)" = 2
    test "$(cat /run/specgate/components/nginx)" = failed
    /usr/local/bin/python3 -c 'import importlib.util; s=importlib.util.spec_from_file_location("health", "/source/health_server.py"); m=importlib.util.module_from_spec(s); s.loader.exec_module(m); assert m.component_state("nginx", True) == "ready"; assert m.last_failure()["restart_count"] == 2'
    test ! -e /run/specgate/restarts/nginx
    printf 'protected\\n' >/tmp/protected
    rm /run/specgate/components/nginx
    ln -s /tmp/protected /run/specgate/components/nginx
    if bash /source/service-finish.sh nginx 1 0; then exit 1; fi
    test "$(cat /tmp/protected)" = protected
    rm /run/specgate/components/nginx
    ln -s /tmp/protected /run/specgate/restarts/nginx
    if bash /source/service-finish.sh nginx 1 0; then exit 1; fi
    test "$(cat /tmp/protected)" = protected
    rm /run/specgate/restarts/nginx
    rm /data/diagnostics/last-failure.json
    ln -s /tmp/protected /data/diagnostics/last-failure.json
    bash /source/service-finish.sh nginx 1 0
    test "$(cat /tmp/protected)" = protected
    test ! -L /data/diagnostics/last-failure.json
    printf '4\\n' >/run/specgate/restarts/nginx
    kill() { [[ "$*" = '-TERM 1' ]] && touch /tmp/stop-requested; }
    source /source/service-finish.sh nginx 1 0
    unset -f kill
    test -e /tmp/stop-requested
    test "$(cat /run/specgate/restarts/nginx)" = 5
    printf '#!/bin/sh\nprintf false\\n\n' >/command/s6-svstat
    bash /source/service-finish.sh nginx 0 0
    test "$(cat /run/specgate/components/nginx)" = stopped
    ln -s /tmp/protected /data/diagnostics/legacy-link
    exec() { [[ "$1" = /init ]]; }
    if (source /source/entrypoint.sh); then exit 1; fi
    test "$(cat /tmp/protected)" = protected
    rm /data/diagnostics/legacy-link
    mv /run/specgate/components /run/specgate/components-safe
    ln -s /tmp /run/specgate/components
    if (source /source/entrypoint.sh); then exit 1; fi
    test "$(cat /tmp/protected)" = protected
  `], { encoding: "utf8", timeout: 60000 });
  assert.equal(result.status, 0, result.stdout + result.stderr);
});
