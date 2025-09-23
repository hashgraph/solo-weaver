#!/bin/bash
SCRIPT_PATH="$(cd "$(dirname "${0}")" && pwd)"
docker build -t local/solo-provisioner-local:latest -f "${SCRIPT_PATH}/Dockerfile.local" "${SCRIPT_PATH}"
docker stop solo-provisioner-local || true
docker rm solo-provisioner-local || true
docker run -t --rm --name solo-provisioner-local --privileged --cap-add=ALL -v "/sys/fs/cgroup:/sys/fs/cgroup:rw" -v "${SCRIPT_PATH}"/..:/app local/solo-provisioner-local:latest