#!/bin/bash
SCRIPT_PATH="$(cd "$(dirname "${0}")" && pwd)"
docker build -t local/solo-provisioner-ci:latest -f "${SCRIPT_PATH}/Dockerfile.ci" "${SCRIPT_PATH}"
docker stop solo-provisioner-ci || true
docker rm solo-provisioner-ci || true
docker run -t --rm --name solo-provisioner-ci --privileged --cap-add=ALL -v "/sys/fs/cgroup:/sys/fs/cgroup:rw" -v "${SCRIPT_PATH}"/..:/app local/solo-provisioner-ci:latest