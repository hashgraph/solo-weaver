# Develop

## Run tests

- Launch the container with `./launch.sh`
- In a separate terminal, run `docker exec -it solo-provisioner-test`
- Inside the container, run `/app/bin/provisioner-linux-arm64 system preflight --config /app/test/config.yaml `