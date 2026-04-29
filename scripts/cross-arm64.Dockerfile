# Cross-compile environment for tentacle aarch64 builds.
#
# Mirrors .github/workflows/release.yml's "container: debian:bullseye" so the
# resulting binary links against glibc 2.31 — old enough to run on Debian 11
# devices like the CompuLab IOT-GATE-iMX8. Building on a host with newer glibc
# (e.g. Ubuntu 24.04 = glibc 2.39) produces a binary the gateway can't load.

FROM debian:bullseye

ARG GO_VERSION=1.25.5

RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl git xz-utils \
      cmake build-essential \
      gcc-aarch64-linux-gnu g++-aarch64-linux-gnu \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" \
      | tar -C /usr/local -xz
ENV PATH=/usr/local/go/bin:/root/go/bin:$PATH GOPATH=/root/go

# Pre-build libplctag for arm64 into /opt/libplctag-arm64 so the deploy script
# can just point CGO_LDFLAGS at it without re-running cmake every deploy.
RUN git clone --depth 1 https://github.com/libplctag/libplctag.git /tmp/libplctag \
    && mkdir -p /tmp/libplctag/build-arm64 \
    && cd /tmp/libplctag/build-arm64 \
    && cmake .. \
         -DCMAKE_C_COMPILER=aarch64-linux-gnu-gcc \
         -DCMAKE_CXX_COMPILER=aarch64-linux-gnu-g++ \
         -DCMAKE_SYSTEM_NAME=Linux \
         -DCMAKE_SYSTEM_PROCESSOR=aarch64 \
    && (make -j"$(nproc)" || true) \
    && mkdir -p /opt/libplctag-arm64/lib /opt/libplctag-arm64/include \
    && cp -f bin_dist/libplctag_static.a /opt/libplctag-arm64/lib/ \
    && find /tmp/libplctag/src -name 'libplctag.h' -exec cp {} /opt/libplctag-arm64/include/ \; \
    && rm -rf /tmp/libplctag

WORKDIR /workspace
