# teambitflow/bitflow-collector-build:debian
# docker build -t teambitflow/bitflow-collector-build:debian -f debian-build.Dockerfile .
FROM teambitflow/golang-build:debian
RUN apt install -y libvirt-dev libpcap-dev