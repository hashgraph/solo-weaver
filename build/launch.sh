#!/bin/bash
SCRIPT_PATH="$(cd "$(dirname "${0}")" && pwd)"
docker build -t local/test-container:latest "${SCRIPT_PATH}"
docker run -t --rm --name test --privileged --cap-add=ALL -v "${SCRIPT_PATH}"/..:/app local/test-container:latest
