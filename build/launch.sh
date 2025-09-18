#!/bin/bash
SCRIPT_PATH="$(cd "$(dirname "${0}")" && pwd)"
docker build -t local/solo-provisioner-test:latest "${SCRIPT_PATH}"
docker run -t --rm --name solo-provisioner-test --privileged --cap-add=ALL -v "/sys/fs/cgroup:/sys/fs/cgroup:rw" -v "${SCRIPT_PATH}"/..:/app local/solo-provisioner-test:latest