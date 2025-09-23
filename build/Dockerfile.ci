FROM ubuntu:noble

ENV DEBIAN_FRONTEND=noninteractive
#ENV container docker

RUN apt-get update \
        && apt-get install -y --no-install-recommends sudo systemd systemd-cron systemd-sysv dbus dbus-user-session \
        && apt-get install -y --no-install-recommends openssl ca-certificates curl

# RUN apt-get install -y --no-install-recommends hashdeep incron jq rsync vim tzdata openssl ca-certificates curl unzip zip gettext

#RUN install -m 0755 -d /etc/apt/keyrings && \
#    curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc && \
#    chmod a+r /etc/apt/keyrings/docker.asc && \
#    echo \
#      "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu \
#      $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
#      sudo tee /etc/apt/sources.list.d/docker.list > /dev/null && \
#    apt-get update && \
#    apt-get install -y --no-install-recommends docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

RUN apt-get clean \
        && rm -Rf /usr/share/doc && rm -Rf /usr/share/man \
        && rm -rf /var/lib/apt/lists/* \
        && touch -d "2 hours ago" /var/lib/apt/lists

#RUN sed -i 's/^\($ModLoad imklog\)/#\1/' /etc/rsyslog.conf

RUN rm -f /lib/systemd/system/systemd*udev* \
      && rm -f /lib/systemd/system/getty.target

#RUN echo 'export DBUS_SESSION_BUS_ADDRESS=`dbus-daemon --fork --config-file=/usr/share/dbus-1/session.conf --print-address`' >/etc/profile.d/01-dbus-setup.sh

VOLUME ["/sys/fs/cgroup"]

WORKDIR /opt/container
ADD container-entrypoint.sh .
RUN chmod +x container-entrypoint.sh

ENTRYPOINT ["/bin/bash", "container-entrypoint.sh"]