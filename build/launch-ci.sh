#!/bin/bash
SCRIPT_PATH="$(cd "$(dirname "${0}")" && pwd)"
docker build -t local/solo-weaver-ci:latest -f "${SCRIPT_PATH}/Dockerfile.ci" "${SCRIPT_PATH}"
docker stop solo-weaver-ci || true
docker rm solo-weaver-ci || true
docker run -t --rm --name solo-weaver-ci --privileged --cap-add=ALL -v "/sys/fs/cgroup:/sys/fs/cgroup:rw" -v "${SCRIPT_PATH}"/..:/app local/solo-weaver-ci:latest