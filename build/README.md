# Develop

To develop locally, you need to use the provided Docker setup as below:

- Launch the container with `./launch-local.sh`
- In a separate terminal
    - Exec into the container: `docker exec -it solo-provisioner-local`
    - Build: `cd /app && task build`
    - Run: `task run -- system setup -c test/config/config.yaml`