#!/usr/bin/env bash
# Smoke test of a Verve Docker image. Starts a container on a new named volume, waits for it to
# become healthy, then checks /healthz, the web app and the user the server runs as. The container
# and the volume are always removed.
#
# Usage: scripts/docker-smoke.sh <image>
set -euo pipefail

readonly healthy_timeout=30
readonly expected_user="65532:65532"

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <image>" >&2
  exit 2
fi

readonly image=$1
# Names both the container and its volume.
readonly name="verve-smoke-$$"

cleanup() {
  docker rm --force "$name" >/dev/null 2>&1 || true
  docker volume rm "$name" >/dev/null 2>&1 || true
}

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

fail() {
  echo "FAIL: $image: $*" >&2

  if docker container inspect "$name" >/dev/null 2>&1; then
    echo "--- last container logs ---" >&2
    docker logs --tail 20 "$name" >&2 2>&1 || true
  fi

  exit 1
}

docker volume create "$name" >/dev/null || fail "cannot create the volume"

# A loopback port chosen by Docker, so that the test runs next to anything else on 8080.
docker run --detach --name "$name" --publish 127.0.0.1::8080 --volume "$name:/data" "$image" >/dev/null ||
  fail "cannot start a container"

deadline=$((SECONDS + healthy_timeout))

while :; do
  read -r state health < <(
    docker inspect --format '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$name"
  ) || fail "cannot inspect the container"

  [ "$state" = running ] || fail "container is $state instead of running"
  [ "$health" != none ] || fail "image has no HEALTHCHECK"
  [ "$health" != unhealthy ] || fail "container is unhealthy"
  [ "$health" != healthy ] || break
  [ "$SECONDS" -lt "$deadline" ] || fail "container is still $health after $healthy_timeout seconds"

  sleep 1
done

address=$(docker port "$name" 8080/tcp) || fail "port 8080 is not published"
address=${address%%$'\n'*}

curl --fail --silent --show-error --max-time 5 --output /dev/null "http://$address/healthz" ||
  fail "/healthz does not answer 200"

page=$(curl --fail --silent --show-error --max-time 5 "http://$address/") ||
  fail "/ does not answer 200"

case $page in
  *'<div id="root">'*) ;;
  *) fail "/ does not serve the web app" ;;
esac

# Every process of the container, which a running healthcheck can add to. ps runs on the Docker
# host, where BusyBox has no uid column and the distroless user has no name, so it prints the IDs.
users=$(docker top "$name" -o pid,user,group | awk 'NR > 1 { print $2 ":" $3 }' | sort -u) ||
  fail "cannot list the container processes"

[ "$users" = "$expected_user" ] || fail "server runs as ${users:-nobody} instead of $expected_user"

echo "PASS: $image: healthy, /healthz and / answer 200, running as $expected_user"
