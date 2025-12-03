#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
SCRIPT_PATH="$(cd "$(dirname "${0}")" && pwd)"
docker build -t local/solo-weaver-local:latest -f "${SCRIPT_PATH}/Dockerfile.local" "${SCRIPT_PATH}"
docker stop solo-weaver-local || true
docker rm solo-weaver-local || true
docker run -t --rm --name solo-weaver-local --privileged --cap-add=ALL -v "/sys/fs/cgroup:/sys/fs/cgroup:rw" -v "${SCRIPT_PATH}"/..:/app local/solo-weaver-local:latest